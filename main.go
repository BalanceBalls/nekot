package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/migrations"
	"github.com/BalanceBalls/nekot/util"
	"github.com/BalanceBalls/nekot/views"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/joho/godotenv"
	zone "github.com/lrstanley/bubblezone"
)

var purgeCache bool
var provider string
var baseUrl string
var theme string
var model string
var newSession bool

func init() {
	flag.BoolVar(&purgeCache, "purge-cache", false, "Invalidate models cache")
	flag.BoolVar(&newSession, "n", false, "Create a new session on startup")
	flag.StringVar(
		&provider,
		"p",
		"",
		"Overrides LLM provider configuration. Available: openai, gemini",
	)
	flag.StringVar(&baseUrl, "u", "", "Overrides LLM provider base url configuration")
	flag.StringVar(&theme, "t", "", "Overrides theme configuration")
	flag.StringVar(&model, "m", "", "Model name")
}

func main() {
	flag.Parse()

	var pipedContent string
	stat, _ := os.Stdin.Stat()
	if (stat.Mode() & os.ModeCharDevice) == 0 {
		data, err := io.ReadAll(os.Stdin)
		if err == nil {
			pipedContent = string(data)
		}
	}

	flags := config.StartupFlags{
		Model:           model,
		Theme:           theme,
		Provider:        provider,
		ProviderUrl:     baseUrl,
		StartNewSession: newSession,
		InitialPrompt:   pipedContent,
	}

	env := os.Getenv("NEKOT_ENV")
	if env == "" {
		env = "development"
	}

	godotenv.Load(".env." + env + ".local")
	if env != "test" {
		godotenv.Load(".env.local")
	}
	godotenv.Load(".env." + env)
	godotenv.Load() // The Original .env

	appPath, err := util.GetAppDataPath()
	f, err := tea.LogToFile(filepath.Join(appPath, "debug.log"), "debug")
	if err != nil {
		fmt.Println("fatal:", err)
		os.Exit(1)
	}
	defer f.Close()

	// delete files if in dev mode
	util.DeleteFilesIfDevMode()
	// validate config
	configToUse := config.CreateAndValidateConfig(flags)

	// run migrations for our database
	db := util.InitDb()
	err = util.MigrateFS(db, migrations.FS, ".")
	if err != nil {
		log.Println("Error: ", err)
		panic(err)
	}
	defer db.Close()

	if purgeCache {
		err = util.PurgeModelsCache(db)
		if err != nil {
			log.Println("Failed to purge models cache:", err)
		} else {
			log.Println("Models cache invalidated")
		}
	}

	ctx := context.Background()
	ctxWithConfig := config.WithConfig(ctx, &configToUse)
	appCtx := config.WithFlags(ctxWithConfig, &flags)
	zone.NewGlobal()

	p := tea.NewProgram(
		views.NewMainView(db, appCtx),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)

	_, err = p.Run()
	if err != nil {
		if err == tea.ErrProgramPanic {
			fmt.Fprintf(os.Stderr, "Program panicked: %v\n", err)
			os.Exit(1)
		}
		log.Fatal(err)
	}
}
