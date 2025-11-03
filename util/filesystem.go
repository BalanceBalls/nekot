package util

import (
	"os"
	"path/filepath"
)

func DeleteFilesIfDevMode() {
	if os.Getenv("DEV_MODE") == "true" {
		appPath, err := GetAppDataPath()
		if err != nil {
			panic(err)
		}

		pathToPersistDb := filepath.Join(appPath, "chat.db")
		err = os.Remove(pathToPersistDb)
		if err != nil {
			Slog.Error("failed to delete database file", "error", err)
		}

		pathToPersistedFile := filepath.Join(appPath, "config.json")
		err = os.Remove(pathToPersistedFile)
		if err != nil {
			Slog.Error("failed to delete config file", "error", err)
		}
	}
}
