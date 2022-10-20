package cmd

import "github.com/minio/cli"

var aclSubcommands = []cli.Command{
	aclSetCmd,
	aclGetCmd,
}

var aclCmd = cli.Command{
	Name:            "acl",
	Usage:           "manage acl defined in the MinIO server",
	Action:          mainAcl,
	Before:          setGlobalsFromContext,
	Flags:           globalFlags,
	Subcommands:     aclSubcommands,
	HideHelpCommand: true,
}

func mainAcl(ctx *cli.Context) error {
	commandNotFound(ctx, aclSubcommands)
	return nil
}
