package config

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/BalanceBalls/nekot/util"
)

// Define a type for your context key to avoid collisions with other context keys
type contextKey string

// Define a constant for your config context key
const configKey contextKey = "config"

var TRUE bool = true

// WithConfig returns a new context with the provided config
func WithConfig(ctx context.Context, config *Config) context.Context {
	return context.WithValue(ctx, configKey, config)
}

// FromContext extracts the config from the context, if available
func FromContext(ctx context.Context) (*Config, bool) {
	config, ok := ctx.Value(configKey).(*Config)
	return config, ok
}

type Config struct {
	ChatGPTApiUrl                   string           `json:"chatGPTAPiUrl"`
	ProviderBaseUrl                 string           `json:"providerBaseUrl"`
	SystemMessage                   string           `json:"systemMessage"`
	DefaultModel                    string           `json:"defaultModel"`
	Provider                        string           `json:"provider"`
	ColorScheme                     util.ColorScheme `json:"colorScheme"`
	MaxAttachmentSizeMb             int              `json:"maxAttachmentSizeMb"`
	IncludeReasoningTokensInContext *bool            `json:"includeReasoningTokensInContext"`
}

type StartupFlags struct {
	Theme       string
	Provider    string
	ProviderUrl string
}

//go:embed config.json
var configEmbed embed.FS

func createConfig() (string, error) {
	appPath, err := util.GetAppDataPath()
	if err != nil {
		util.Slog.Error("failed to get app path", "error", err.Error())
		panic(err)
	}

	pathToPersistedFile := filepath.Join(appPath, "config.json")

	if _, err := os.Stat(pathToPersistedFile); os.IsNotExist(err) {
		// The database does not exist, extract from embedded
		configFile, err := configEmbed.Open("config.json")
		if err != nil {
			return "", err
		}
		defer configFile.Close()

		// Ensure the directory exists
		if err := os.MkdirAll(filepath.Dir(pathToPersistedFile), 0755); err != nil {
			return "", err
		}

		// Create the persistent file
		outFile, err := os.Create(pathToPersistedFile)
		if err != nil {
			return "", err
		}
		defer outFile.Close()

		// Copy the embedded database to the persistent file
		if _, err := io.Copy(outFile, configFile); err != nil {
			return "", err
		}
	} else if err != nil {
		// An error occurred checking for the file, unrelated to file existence
		return "", err
	}

	return pathToPersistedFile, nil
}

func validateConfig(config Config) bool {
	switch config.Provider {
	case util.GeminiProviderType:
		return true
	case util.OpenAiProviderType:
		// Validate provider base url format
		match, _ := regexp.MatchString(`^https?://`, config.ProviderBaseUrl)
		if !match {
			fmt.Println("ProviderBaseUrl must be a valid URL")
			return false
		}
		// Add any other validation logic here
		return true
	default:
		fmt.Println("Incorrect provider type. Supported values: 'openai', 'gemini'")
		return false
	}
}

func CreateAndValidateConfig(flags StartupFlags) Config {
	configFilePath, err := createConfig()
	if err != nil {
		fmt.Printf("Error finding config JSON: %s", err)
		panic(err)
	}

	content, err := os.ReadFile(configFilePath)
	if err != nil {
		fmt.Printf("Error reading config JSON: %s", err)
		panic(err)
	}

	var config Config

	err = json.Unmarshal(content, &config)
	if err != nil {
		fmt.Printf("Error parsing config JSON: %s", err)
		panic(err)
	}

	config.setDefaults()
	config.applyFlags(flags)

	isValidConfig := validateConfig(config)
	if !isValidConfig {
		panic(fmt.Errorf("Invalid config"))
	}

	config.checkApiKeys()

	return config
}

func (c Config) checkApiKeys() {
	switch c.Provider {
	case util.GeminiProviderType:
		apiKey := os.Getenv("GEMINI_API_KEY")
		if apiKey == "" {
			fmt.Println("GEMINI_API_KEY not set; set it in your profile")
			fmt.Printf(
				"export GEMINI_API_KEY=your_key in the config for :%v \n",
				os.Getenv("SHELL"),
			)
			fmt.Println("Exiting...")
			os.Exit(1)
		}
	case util.OpenAiProviderType:
		apiKey := os.Getenv("OPENAI_API_KEY")
		if apiKey == "" {
			fmt.Println("OPENAI_API_KEY not set; set it in your profile")
			fmt.Printf(
				"export OPENAI_API_KEY=your_key in the config for :%v \n",
				os.Getenv("SHELL"),
			)
			fmt.Println("Exiting...")
			os.Exit(1)
		}
	}
}

func (c *Config) setDefaults() {
	// for backwards compatibility
	if c.ProviderBaseUrl == "" {
		c.ProviderBaseUrl = c.ChatGPTApiUrl
	}

	if c.Provider == "" {
		c.Provider = util.OpenAiProviderType
	}

	if c.MaxAttachmentSizeMb == 0 {
		c.MaxAttachmentSizeMb = 3
	}

	if c.IncludeReasoningTokensInContext == nil {
		c.IncludeReasoningTokensInContext = &TRUE
	}
}

func (c *Config) applyFlags(flags StartupFlags) {
	if flags.Theme != "" {
		c.ColorScheme = util.ColorScheme(strings.ToLower(flags.Theme))
	}

	if flags.Provider != "" {
		c.Provider = flags.Provider
	}

	if flags.ProviderUrl != "" {
		c.ProviderBaseUrl = flags.ProviderUrl
	}
}
