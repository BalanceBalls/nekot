package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tearingItUp786/nekot/clients"
	"github.com/tearingItUp786/nekot/config"
	"github.com/tearingItUp786/nekot/util"
)

// const ModelsCacheTtl = time.Second * 5

const ModelsCacheTtl = time.Hour * 24 * 14 // 14 days
const ModelsSeparator = ";"
const DateLayout = "2006-01-02 15:04:05"

const defaultMaxTokens = 3000

type SettingsService struct {
	DB *sql.DB
}

func NewSettingsService(db *sql.DB) *SettingsService {
	return &SettingsService{
		DB: db,
	}
}

func (ss *SettingsService) GetPreset(id int) (util.Settings, error) {
	settings := util.Settings{}
	row := ss.DB.QueryRow(
		`select 
			settings_id,
			settings_model,
			settings_max_tokens,
			settings_frequency,
			system_msg,
			top_p,
			temperature,
			preset_name
		from settings where settings_id=$1`,
		id,
	)
	err := row.Scan(
		&settings.ID,
		&settings.Model,
		&settings.MaxTokens,
		&settings.Frequency,
		&settings.SystemPrompt,
		&settings.TopP,
		&settings.Temperature,
		&settings.PresetName,
	)

	if err != nil {
		return settings, err
	}

	return settings, nil
}

func (ss *SettingsService) GetSettings(ctx context.Context, id int, cfg config.Config) tea.Msg {
	settings := util.Settings{}
	row := ss.DB.QueryRow(
		`select 
			settings_id,
			settings_model,
			settings_max_tokens,
			settings_frequency,
			system_msg,
			top_p,
			temperature,
			preset_name
		from settings where settings_id=$1`,
		id,
	)
	err := row.Scan(
		&settings.ID,
		&settings.Model,
		&settings.MaxTokens,
		&settings.Frequency,
		&settings.SystemPrompt,
		&settings.TopP,
		&settings.Temperature,
		&settings.PresetName,
	)

	availableModels, modelsError := ss.GetProviderModels(ctx, cfg.Provider, cfg.ProviderBaseUrl)

	if modelsError != nil {
		return UpdateSettingsEvent{
			Settings: settings,
			Err:      modelsError,
		}
	}

	isModelFromSettingsAvailable := slices.Contains(availableModels, settings.Model)

	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return UpdateSettingsEvent{
				Settings: settings,
				Err:      err,
			}
		}

		settings = util.Settings{
			Model:     availableModels[0],
			MaxTokens: 3000,
		}

		// if default model is set in config.json - use it instead
		if len(cfg.DefaultModel) > 0 {
			settings.Model = cfg.DefaultModel
		}
	}

	if !isModelFromSettingsAvailable && len(availableModels) > 0 {
		modelIdx := rand.IntN(len(availableModels) - 1)
		settings.Model = availableModels[modelIdx]
		ss.UpdateSettings(settings)
	}

	return UpdateSettingsEvent{
		Settings: settings,
		Err:      nil,
	}
}

func (ss *SettingsService) GetProviderModels(ctx context.Context, providerType string, apiUrl string) ([]string, error) {
	provider := util.GetOpenAiInferenceProvider(providerType, apiUrl)
	availableModels := []string{}

	if provider != util.Local {
		var cacheErr error
		availableModels, cacheErr = ss.TryGetModelsCache(int(provider))
		if cacheErr != nil {
			log.Println("Faild to get models cache: ", cacheErr)
		}
	}

	if len(availableModels) == 0 {
		llmClient := clients.ResolveLlmClient(providerType, apiUrl, "")
		modelsResponse := llmClient.RequestModelsList(ctx)
		if modelsResponse.Err != nil {
			return []string{}, modelsResponse.Err
		}

		availableModels = util.GetFilteredModelList(providerType, apiUrl, modelsResponse.Result.GetModelNamesFromResponse())

		if provider == util.Local {
			return availableModels, nil
		}

		err := ss.CacheModelsForProvider(int(provider), availableModels)
		if err != nil {
			log.Println("Cache update error:", err)
		}
	}

	return availableModels, nil
}

func (ss *SettingsService) TryGetModelsCache(provider int) ([]string, error) {
	var cachedModels string
	var cachedAt string
	row := ss.DB.QueryRow(
		`select models, cached_at from models where provider = $1`,
		provider,
	)
	err := row.Scan(&cachedModels, &cachedAt)

	if err != nil {
		return []string{}, err
	}

	expireDate := time.Now().UTC().Add(-ModelsCacheTtl)
	parsedDate, err := time.Parse(DateLayout, cachedAt)

	if err == nil && parsedDate.Before(expireDate) {
		return []string{}, errors.New("Models cache expired")
	}

	modelsList := strings.Split(cachedModels, ModelsSeparator)
	filteredModels := []string{}
	for _, model := range modelsList {
		if len(model) != 0 {
			filteredModels = append(filteredModels, model)
		}
	}

	return filteredModels, nil
}

func (ss *SettingsService) CacheModelsForProvider(provider int, models []string) error {
	mergedString := strings.Join(models, ModelsSeparator)

	upsert := `
		INSERT INTO models
			(provider, models, cached_at)
		VALUES
			($1, $2, $3)
		ON CONFLICT(provider) DO UPDATE SET
			models=$2,
			cached_at=$3;
	`

	_, err := ss.DB.Exec(
		upsert,
		provider,
		mergedString,
		time.Now().UTC().Format(DateLayout),
	)
	return err
}

func (ss *SettingsService) GetPresetsList() ([]util.Settings, error) {
	rows, err := ss.DB.Query(
		`select 
			settings_id,
			settings_model,
			settings_max_tokens,
			settings_frequency,
			system_msg,
			top_p,
			temperature,
			preset_name
		from settings`,
	)

	if err != nil {
		return []util.Settings{}, err
	}
	presets := []util.Settings{}
	for rows.Next() {
		preset := util.Settings{}
		rows.Scan(&preset.ID, &preset.Model, &preset.MaxTokens, &preset.Frequency, &preset.SystemPrompt, &preset.TopP, &preset.Temperature, &preset.PresetName)
		presets = append(presets, preset)
	}
	defer rows.Close()

	return presets, nil
}

func (ss *SettingsService) ResetToDefault(current util.Settings) (util.Settings, error) {
	defaultSettings := util.Settings{
		ID:           current.ID,
		Model:        current.Model,
		MaxTokens:    defaultMaxTokens,
		Frequency:    nil,
		SystemPrompt: current.SystemPrompt,
		TopP:         nil,
		Temperature:  nil,
		PresetName:   current.PresetName,
	}

	_, err := ss.UpdateSettings(defaultSettings)

	if err != nil {
		return current, err
	}

	return defaultSettings, nil
}

func (ss *SettingsService) SavePreset(newSettings util.Settings) (int, error) {
	upsert := `
		INSERT INTO settings 
			(settings_model, settings_max_tokens, settings_frequency, temperature, top_p, system_msg, preset_name)
		VALUES
			($1, $2, $3, $4, $5, $6, $7)
		RETURNING settings_id
	`

	result, err := ss.DB.Exec(
		upsert,
		newSettings.Model,
		newSettings.MaxTokens,
		newSettings.Frequency,
		newSettings.Temperature,
		newSettings.TopP,
		newSettings.SystemPrompt,
		newSettings.PresetName,
	)

	errId := -999999
	if err != nil {
		return errId, err
	}
	newId, err := result.LastInsertId()
	if err != nil {
		return errId, fmt.Errorf("Failed to get last inserted id")
	}
	return int(newId), nil
}

func (ss *SettingsService) RemovePreset(id int) error {
	existing, err := ss.GetPresetsList()

	if err != nil {
		return err
	}

	if len(existing) == 1 {
		return errors.New("Cannot remove the only settings preset")
	}

	_, err = ss.DB.Exec(`delete from settings where settings_id=$1;`, id)
	return err
}

func (ss *SettingsService) UpdateSettings(newSettings util.Settings) (util.Settings, error) {
	upsert := `
		INSERT INTO settings 
			(settings_id, settings_model, settings_max_tokens, settings_frequency, temperature, top_p, system_msg, preset_name)
		VALUES
			($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT(settings_id) DO UPDATE SET
			settings_model=$2,
			settings_max_tokens=$3,
			settings_frequency=$4,
			temperature=$5,
			top_p=$6,
			system_msg=$7,
			preset_name=$8;
	`

	_, err := ss.DB.Exec(
		upsert,
		newSettings.ID,
		newSettings.Model,
		newSettings.MaxTokens,
		newSettings.Frequency,
		newSettings.Temperature,
		newSettings.TopP,
		newSettings.SystemPrompt,
		newSettings.PresetName,
	)
	if err != nil {
		return newSettings, err
	}
	return newSettings, nil
}
