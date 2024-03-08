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

	"github.com/tearingItUp786/chatgpt-tui/util"
)

// Define a type for your context key to avoid collisions with other context keys
type contextKey string

// Define a constant for your config context key
const configKey contextKey = "config"

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
	ChatGPTApiUrl string `json:"chatGPTAPiUrl"`
	SystemMessage string `json:"systemMessage"`
}

//go:embed config.json
var configEmbed embed.FS

func createConfig() (string, error) {
	appPath, err := util.GetAppDataPath()
	if err != nil {
		fmt.Println("Error getting app path:", err)
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
	// Validate the ChatAPIURL format (simple example)
	match, _ := regexp.MatchString(`^https?://`, config.ChatGPTApiUrl)
	if !match {
		fmt.Println("ChatAPIURL must be a valid URL")
		return false
	}
	// Add any other validation logic here
	return true
}

func CreateAndValidateConfig() Config {
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

	isValidConfig := validateConfig(config)
	if !isValidConfig {
		panic(fmt.Errorf("Invalid config"))
	}

	return config
}
