package cmd

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/nextlevelbuilder/goclaw/internal/config"
	"github.com/nextlevelbuilder/goclaw/internal/store/pg"
)

// Secrets live encrypted in config_secrets, so they cannot be seeded with a
// plain SQL INSERT, and the admin UI only lists the providers it was built
// with. This subcommand is the missing headless path: set any config secret
// (a web_search provider key, for instance) on a server with no UI.
func configSecretCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "secret",
		Short: "Manage encrypted config secrets",
	}
	cmd.AddCommand(configSecretSetCmd())
	cmd.AddCommand(configSecretListCmd())
	return cmd
}

func configSecretSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Store an encrypted config secret",
		Long: "Store an encrypted config secret.\n\n" +
			"Web search provider keys use tools.web.<provider>.api_key, e.g.\n" +
			"  goclaw config secret set tools.web.tavily.api_key tvly-xxxx\n" +
			"  goclaw config secret set tools.web.google_cse.api_key AIzaXXXX:cx-id\n\n" +
			"Google CSE needs both the API key and the search engine id; pass them\n" +
			"as APIKEY:CX (the provider splits on the first colon).",
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			secrets, db, err := openSecretsStore()
			if err != nil {
				return err
			}
			defer db.Close()

			key := strings.TrimSpace(args[0])
			if err := secrets.Set(context.Background(), key, args[1]); err != nil {
				return fmt.Errorf("store secret: %w", err)
			}
			fmt.Printf("Stored %s (%d chars, encrypted).\n", key, len(args[1]))
			return nil
		},
	}
}

func configSecretListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List config secret keys (values never printed)",
		RunE: func(cmd *cobra.Command, args []string) error {
			secrets, db, err := openSecretsStore()
			if err != nil {
				return err
			}
			defer db.Close()

			all, err := secrets.GetAll(context.Background())
			if err != nil {
				return fmt.Errorf("read secrets: %w", err)
			}
			if len(all) == 0 {
				fmt.Println("No config secrets stored.")
				return nil
			}
			for key, value := range all {
				fmt.Printf("  %-40s (%d chars)\n", key, len(value))
			}
			return nil
		},
	}
}

func openSecretsStore() (*pg.PGConfigSecretsStore, *sql.DB, error) {
	cfg, err := config.Load(resolveConfigPath())
	if err != nil {
		return nil, nil, fmt.Errorf("load config: %w", err)
	}
	if cfg.Database.PostgresDSN == "" {
		return nil, nil, fmt.Errorf("postgres DSN not configured; set GOCLAW_POSTGRES_DSN")
	}
	db, err := sql.Open("pgx", cfg.Database.PostgresDSN)
	if err != nil {
		return nil, nil, fmt.Errorf("open db: %w", err)
	}
	return pg.NewPGConfigSecretsStore(db, os.Getenv("GOCLAW_ENCRYPTION_KEY")), db, nil
}
