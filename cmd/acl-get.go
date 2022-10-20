package cmd

import (
	"context"
	"encoding/json"
	"os"

	"github.com/minio/cli"
	"github.com/minio/mc/pkg/probe"
)

var aclGetFlags = []cli.Flag{
	cli.StringFlag{
		Name:  "acl-file, f",
		Usage: "additionally (over-)write acl JSON to given file",
	},
}

var aclGetCmd = cli.Command{
	Name:         "get",
	Usage:        "get acl on a bucket/object",
	Action:       mainAclGet,
	OnUsageError: onUsageError,
	Before:       setGlobalsFromContext,
	Flags:        append(aclGetFlags, globalFlags...),
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} [FLAGS] TARGET

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Show information on a given bucket or object.
     {{.Prompt}} {{.HelpName}} myminio/mybucket/myobject

  2. Show information on a given bucket or object and write the acl JSON content to /tmp/policy.json.
     {{.Prompt}} {{.HelpName}} myminio/mybucket/myobject --acl-file /tmp/policy.json
`,
}

// Validate command line arguments.
func checkAclGetSyntax(cliCtx *cli.Context) {
	if !cliCtx.Args().Present() {
		cli.ShowCommandHelpAndExit(cliCtx, "get", 1) // last argument is exit code
	}
}

func mainAclGet(cli *cli.Context) error {
	checkAclGetSyntax(cli)

	args := cli.Args()

	targetURL := args.Get(0)

	clnt, err := newClient(targetURL)
	fatalIf(err.Trace(targetURL), "Invalid target `"+targetURL+"`.")

	ctx, cancelAclGet := context.WithCancel(globalContext)
	defer cancelAclGet()

	acld, err := clnt.AclGet(ctx)
	fatalIf(err.Trace(targetURL), "Unable to get ACL `"+targetURL+"`.")

	aclFile := cli.String("acl-file")
	if aclFile != "" {
		f, err := os.Create(aclFile)
		if err != nil {
			fatalIf(probe.NewError(err).Trace(args...), "Could not open given acl file")
		}
		acl, err := json.MarshalIndent(acld, "", " ")
		if err != nil {
			fatalIf(probe.NewError(err).Trace(args...), "Marshal failed")
		}
		_, err = f.Write(acl)
		if err != nil {
			fatalIf(probe.NewError(err).Trace(args...), "Could not write to given acl file")
		}
	}

	printMsg(userAclMessage{
		op:   "get",
		Path: targetURL,
		Acl:  *acld,
	})

	return nil
}
