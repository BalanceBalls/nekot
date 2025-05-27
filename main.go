package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/BalanceBalls/nekot/config"
	"github.com/BalanceBalls/nekot/migrations"
	"github.com/BalanceBalls/nekot/util"
	"github.com/BalanceBalls/nekot/views"
	"github.com/joho/godotenv"
)

var purgeCache bool
var provider string
var baseUrl string
var theme string

func init() {
	flag.BoolVar(&purgeCache, "purge-cache", false, "Invalidate models cache")
	flag.StringVar(&provider, "p", "", "Overrides LLM provider configuration. Available: openai, gemini")
	flag.StringVar(&baseUrl, "u", "", "Overrides LLM provider base url configuration")
	flag.StringVar(&theme, "t", "", "Overrides theme configuration")
}

func main() {
	flag.Parse()

	flags := config.StartupFlags{
		Theme:       theme,
		Provider:    provider,
		ProviderUrl: baseUrl,
	}

	env := os.Getenv("FOO_ENV")
	if "" == env {
		env = "development"
	}

	godotenv.Load(".env." + env + ".local")
	if "test" != env {
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

	p := tea.NewProgram(
		views.NewMainView(db, ctxWithConfig),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err = p.Run()
	if err != nil {
		log.Fatal(err)
	}
}
