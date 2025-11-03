package util

import (
	"log/slog"
	"os"
	"path"
	"path/filepath"
)

var Slog *slog.Logger

func init() {

	appPath, err := GetAppDataPath()
	if err != nil {
		panic(err)
	}
	logFile, err := os.OpenFile(
		filepath.Join(appPath, "debug.log"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND,
		0666,
	)
	if err != nil {
		panic(err)
	}

	opts := slog.HandlerOptions{
		Level:     slog.LevelInfo,
		AddSource: true,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.SourceKey {
				s := a.Value.Any().(*slog.Source)
				s.File = path.Base(s.File)
			}
			return a
		},
	}

	handler := slog.NewTextHandler(logFile, &opts)

	Slog = slog.New(handler)
}
