package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/tamnd/arctic-cli/arctic"
)

func (a *App) publishCmd() *cobra.Command {
	var fromS, toS, kind, repo string
	var commit, private, keep bool
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Convert a month range and upload the shards to Hugging Face",
		Long: "publish processes the pulled months into Parquet and uploads them to a\n" +
			"Hugging Face dataset. It reads the token from HF_TOKEN. Without --commit it\n" +
			"runs the full pipeline but skips the upload, which is the way to rehearse a\n" +
			"run. A stalled commit exits 75 so a supervisor can restart and resume.",
		RunE: func(cmd *cobra.Command, args []string) error {
			types, err := parseTypes(kind)
			if err != nil {
				return codeError(exitUsage, err)
			}
			from, err := arctic.ParseMonth(fromS)
			if err != nil {
				return codeError(exitUsage, err)
			}
			to, err := arctic.ParseMonth(toS)
			if err != nil {
				return codeError(exitUsage, err)
			}
			if to.Before(from) {
				return codeError(exitUsage, fmt.Errorf("--to %s is before --from %s", toS, fromS))
			}
			if repo != "" {
				a.cfg.HFRepo = repo
			}
			if commit && os.Getenv("HF_TOKEN") == "" {
				return codeError(exitUsage, fmt.Errorf("--commit needs HF_TOKEN in the environment"))
			}
			opts := arctic.PublishOptions{
				From:     from,
				To:       to,
				Types:    types,
				HFCommit: commit,
				Private:  private,
				Keep:     keep,
			}
			err = arctic.Publish(cmd.Context(), a.cfg, opts, func(msg string) {
				a.progressf("%s", msg)
			})
			if err != nil {
				return mapErr(err)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&fromS, "from", arctic.CatalogStart().String(), "first month (YYYY-MM)")
	cmd.Flags().StringVar(&toS, "to", arctic.CatalogEnd().String(), "last month (YYYY-MM)")
	cmd.Flags().StringVar(&kind, "type", "both", "comments|submissions|both")
	cmd.Flags().StringVar(&repo, "repo", "", "Hugging Face dataset repo (default: "+arctic.DefaultHFRepo+")")
	cmd.Flags().BoolVar(&commit, "commit", false, "upload to Hugging Face (default: dry run)")
	cmd.Flags().BoolVar(&private, "private", false, "create the dataset repo as private")
	cmd.Flags().BoolVar(&keep, "keep", false, "keep local Parquet after a successful commit")
	return cmd
}
