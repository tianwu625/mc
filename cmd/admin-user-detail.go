// Copyright (c) 2015-2021 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package cmd

import (
	"github.com/fatih/color"
	"github.com/minio/cli"
	"github.com/minio/mc/pkg/probe"
	"github.com/minio/pkg/console"
	"strconv"
)

var adminUserDetailCmd = cli.Command{
	Name:         "detail",
	Usage:        "display additional info of a user",
	Action:       mainAdminUserDetail,
	OnUsageError: onUsageError,
	Before:       setGlobalsFromContext,
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} TARGET USERNAME

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Display the detail of a user "foobar".
     {{.Prompt}} {{.HelpName}} myminio foobar
`,
}

// checkAdminUserAddSyntax - validate all the passed arguments
func checkAdminUserDetailSyntax(ctx *cli.Context) {
	if len(ctx.Args()) != 2 {
		cli.ShowCommandHelpAndExit(ctx, "detail", 1) // last argument is exit code
	}
}

// mainAdminUserInfo is the handler for "mc admin user info" command.
func mainAdminUserDetail(ctx *cli.Context) error {
	checkAdminUserDetailSyntax(ctx)

	console.SetColor("UserMessage", color.New(color.FgGreen))

	// Get the alias parameter from cli
	args := ctx.Args()
	aliasedURL := args.Get(0)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	user, e := client.GetUserDetail(globalContext, args.Get(1))
	fatalIf(probe.NewError(e).Trace(args...), "Unable to get user info")

	sgidStrs := make([]string, 0, len(user.Sgids))
	for _, gid := range user.Sgids {
		sgidStrs = append(sgidStrs, strconv.FormatInt(int64(gid), 10))
	}

	printMsg(userMessage{
		op:          "detail",
		AccessKey:   args.Get(1),
		UserStatus:  string(user.Status),
		CanonicalID: user.CanonicalID,
		Pgid:        strconv.FormatInt(int64(user.Pgid), 10),
		Uid:         strconv.FormatInt(int64(user.Uid), 10),
		Sgids:       sgidStrs,
	})

	return nil
}
