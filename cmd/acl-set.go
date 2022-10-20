package cmd

import (
	"context"
	"fmt"
	"io/ioutil"

	"github.com/fatih/color"
	"github.com/minio/cli"
	json "github.com/minio/colorjson"
	"github.com/minio/mc/pkg/probe"
	"github.com/minio/minio-go/v7"
	"github.com/minio/pkg/console"
)

var aclSetCmd = cli.Command{
	Name:         "set",
	Usage:        "set acl to a bucket/object",
	Action:       mainAclSet,
	OnUsageError: onUsageError,
	Before:       setGlobalsFromContext,
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}
USAGE:
  {{.HelpName}} TARGET ACLFILE
ACLFILE:
  Name of the acl file associated with the bucket or object
  Content of file must be acl with json format
FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Set a new acl of JSON DATA to bucket or object
     {{.Prompt}} {{.HelpName}} myminio/mybucket/myobject /tmp/acl.json`,
}

func checkAclSetSyntax(ctx *cli.Context) {
	if len(ctx.Args()) != 2 {
		cli.ShowCommandHelpAndExit(ctx, "set", 1)
	}
}

type userAclMessage struct {
	op     string
	Status string                          `json:"status"`
	Path   string                          `json:"Path,omitempty"`
	Acl    minio.AccessControlPolicyDecode `json:"AclInfo,omitempty"`
}

func (u userAclMessage) String() string {
	switch u.op {
	case "get":
		buf, e := json.MarshalIndent(u.Acl, "", " ")
		fatalIf(probe.NewError(e), "Unable to marshal to JSON.")
		return string(buf)
	case "set":
		return console.Colorize("AclMessage", fmt.Sprintf("Acl is %s on %s", u.op, u.Path))
	}

	return ""
}

func (u userAclMessage) JSON() string {
	u.Status = "success"
	jsonMessageBytes, e := json.MarshalIndent(u, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")

	return string(jsonMessageBytes)
}

func mainAclSet(cli *cli.Context) error {
	checkAclSetSyntax(cli)

	console.SetColor("AclMessage", color.New(color.FgGreen))

	args := cli.Args()

	targetURL := args.Get(0)

	aclbytes, e := ioutil.ReadFile(args.Get(1))
	fatalIf(probe.NewError(e).Trace(args...), "Unable to get acl")

	clnt, err := newClient(targetURL)
	fatalIf(err, "Invalid target `"+targetURL+"`.")

	ctx, cancelAclSet := context.WithCancel(globalContext)
	defer cancelAclSet()

	err = clnt.AclSet(ctx, string(aclbytes))
	fatalIf(err, "Set acl `"+targetURL+"` failed")

	printMsg(userAclMessage{
		op:   "set",
		Path: args.Get(0),
	})

	return nil
}
