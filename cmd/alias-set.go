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
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/minio/cli"
	"github.com/minio/mc/pkg/probe"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/minio/pkg/console"
	"github.com/minio/pkg/env"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/term"
)

const cred = "YellowItalics"

var aliasSetFlags = []cli.Flag{
	cli.StringFlag{
		Name:  "path",
		Value: "auto",
		Usage: "bucket path lookup supported by the server. Valid options are '[auto, on, off]'",
	},
	cli.StringFlag{
		Name:  "api",
		Usage: "API signature. Valid options are '[S3v4, S3v2]'",
	},
	cli.StringFlag{
		Name:  "type",
		Value: "auto",
		Usage: "credentials type. Valid options are '[auto, normal, ldap]'",
	},
}

var aliasSetCmd = cli.Command{
	Name:      "set",
	ShortName: "s",
	Usage:     "set a new alias to configuration file",
	Action: func(cli *cli.Context) error {
		return mainAliasSet(cli, false)
	},
	OnUsageError:    onUsageError,
	Before:          setGlobalsFromContext,
	Flags:           append(aliasSetFlags, globalFlags...),
	HideHelpCommand: true,
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}
USAGE:
  {{.HelpName}} ALIAS URL ACCESSKEY SECRETKEY
FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Add MinIO service under "myminio" alias. For security reasons turn off bash history momentarily.
     {{.DisableHistory}}
     {{.Prompt}} {{.HelpName}} myminio http://localhost:9000 minio minio123
     {{.EnableHistory}}
  2. Add MinIO service under "myminio" alias, to use dns style bucket lookup. For security reasons
     turn off bash history momentarily.
     {{.DisableHistory}}
     {{.Prompt}} {{.HelpName}} myminio http://localhost:9000 minio minio123 --api "s3v4" --path "off"
     {{.EnableHistory}}
  3. Add Amazon S3 storage service under "mys3" alias. For security reasons turn off bash history momentarily.
     {{.DisableHistory}}
     {{.Prompt}} {{.HelpName}} mys3 https://s3.amazonaws.com \
                 BKIKJAA5BMMU2RHO6IBB V8f1CwQqAcwo80UEIJEjc5gVQUSSx5ohQ9GSrr12
     {{.EnableHistory}}
  4. Add Amazon S3 storage service under "mys3" alias, prompting for keys.
     {{.Prompt}} {{.HelpName}} mys3 https://s3.amazonaws.com --api "s3v4" --path "off"
     Enter Access Key: BKIKJAA5BMMU2RHO6IBB
     Enter Secret Key: V8f1CwQqAcwo80UEIJEjc5gVQUSSx5ohQ9GSrr12
  5. Add Amazon S3 storage service under "mys3" alias using piped keys.
     {{.DisableHistory}}
     {{.Prompt}} echo -e "BKIKJAA5BMMU2RHO6IBB\nV8f1CwQqAcwo80UEIJEjc5gVQUSSx5ohQ9GSrr12" | \
                 {{.HelpName}} mys3 https://s3.amazonaws.com --api "s3v4" --path "off"
     {{.EnableHistory}}
`,
}

// checkAliasSetSyntax - verifies input arguments to 'alias set'.
func checkAliasSetSyntax(ctx *cli.Context, accessKey string, secretKey string, deprecated bool) {
	args := ctx.Args()
	argsNr := len(args)

	if argsNr == 0 {
		cli.ShowCommandHelpAndExit(ctx, ctx.Command.Name, 1) // last argument is exit code
	}

	if argsNr > 4 || argsNr < 2 {
		fatalIf(errInvalidArgument().Trace(ctx.Args().Tail()...),
			"Incorrect number of arguments for alias set command.")
	}

	alias := cleanAlias(args.Get(0))
	url := args.Get(1)
	api := ctx.String("api")
	path := ctx.String("path")
	bucketLookup := ctx.String("lookup")

	if !isValidAlias(alias) {
		fatalIf(errInvalidAlias(alias), "Invalid alias.")
	}

	if !isValidHostURL(url) {
		fatalIf(errInvalidURL(url), "Invalid URL.")
	}

	if !isValidAccessKey(accessKey) {
		fatalIf(errInvalidArgument().Trace(accessKey),
			"Invalid access key `"+accessKey+"`.")
	}

	if !isValidSecretKey(secretKey) {
		fatalIf(errInvalidArgument().Trace(secretKey),
			"Invalid secret key `"+secretKey+"`.")
	}

	if api != "" && !isValidAPI(api) { // Empty value set to default "S3v4".
		fatalIf(errInvalidArgument().Trace(api),
			"Unrecognized API signature. Valid options are `[S3v4, S3v2]`.")
	}

	if deprecated {
		if !isValidLookup(bucketLookup) {
			fatalIf(errInvalidArgument().Trace(bucketLookup),
				"Unrecognized bucket lookup. Valid options are `[dns,auto, path]`.")
		}
	} else {
		if !isValidPath(path) {
			fatalIf(errInvalidArgument().Trace(bucketLookup),
				"Unrecognized path value. Valid options are `[auto, on, off]`.")
		}
	}
}

// setAlias - set an alias config.
func setAlias(alias string, aliasCfgV10 aliasConfigV10) aliasMessage {
	mcCfgV10, err := loadMcConfig()
	fatalIf(err.Trace(globalMCConfigVersion), "Unable to load config `"+mustGetMcConfigPath()+"`.")

	// Add new host.
	mcCfgV10.Aliases[alias] = aliasCfgV10

	err = saveMcConfig(mcCfgV10)
	fatalIf(err.Trace(alias), "Unable to update hosts in config version `"+mustGetMcConfigPath()+"`.")

	return aliasMessage{
		Alias:     alias,
		URL:       aliasCfgV10.URL,
		AccessKey: aliasCfgV10.AccessKey,
		SecretKey: aliasCfgV10.SecretKey,
		API:       aliasCfgV10.API,
		Path:      aliasCfgV10.Path,
	}
}

// probeS3Signature - auto probe S3 server signature: issue a Stat call
// using v4 signature then v2 in case of failure.
func probeS3Signature(ctx context.Context, accessKey, secretKey, sessionToken, url string, peerCert *x509.Certificate) (string, *probe.Error) {
	probeBucketName := randString(60, rand.NewSource(time.Now().UnixNano()), "probe-bucket-sign-")
	// Test s3 connection for API auto probe
	s3Config := &Config{
		// S3 connection parameters
		Insecure:     globalInsecure,
		AccessKey:    accessKey,
		SecretKey:    secretKey,
		SessionToken: sessionToken,
		HostURL:      urlJoinPath(url, probeBucketName),
		Debug:        globalDebug,
	}
	if peerCert != nil {
		configurePeerCertificate(s3Config, peerCert)
	}

	probeSignatureType := func(stype string) (string, *probe.Error) {
		s3Config.Signature = stype
		s3Client, err := S3New(s3Config)
		if err != nil {
			return "", err
		}

		if _, err := s3Client.Stat(ctx, StatOptions{}); err != nil {
			e := err.ToGoError()
			if _, ok := e.(BucketDoesNotExist); ok {
				// Bucket doesn't exist, means signature probing worked successfully.
				return stype, nil
			}
			// AccessDenied means Stat() is not allowed but credentials are valid.
			// AccessDenied is only returned when policy doesn't allow HeadBucket
			// operations.
			if minio.ToErrorResponse(err.ToGoError()).Code == "AccessDenied" {
				return stype, nil
			}

			// For any other errors we fail.
			return "", err.Trace(s3Config.Signature)
		}
		return stype, nil
	}

	stype, err := probeSignatureType("s3v4")
	if err != nil {
		if stype, err = probeSignatureType("s3v2"); err != nil {
			return "", err.Trace("s3v4", "s3v2")
		}
		return stype, nil
	}
	return stype, nil
}

// BuildS3Config constructs an S3 Config and does
// signature auto-probe when needed.
func BuildS3Config(ctx context.Context, url, alias, accessKey, secretKey, sessionToken, api, path string, peerCert *x509.Certificate) (*Config, *probe.Error) {
	s3Config := NewS3Config(url, &aliasConfigV10{
		AccessKey:    accessKey,
		SecretKey:    secretKey,
		SessionToken: sessionToken,
		URL:          url,
		Path:         path,
	})

	if peerCert != nil {
		configurePeerCertificate(s3Config, peerCert)
	}

	// If api is provided we do not auto probe signature, this is
	// required in situations when signature type is provided by the user.
	if api != "" {
		s3Config.Signature = api
		return s3Config, nil
	}
	// Probe S3 signature version
	api, err := probeS3Signature(ctx, accessKey, secretKey, sessionToken, url, peerCert)
	if err != nil {
		return nil, err.Trace(url, accessKey, secretKey, api, path)
	}

	s3Config.Signature = api
	// Success.
	return s3Config, nil
}

// fetchAliasKeys - returns the user accessKey and secretKey
func fetchAliasKeys(args cli.Args) (string, string) {
	accessKey := ""
	secretKey := ""
	console.SetColor(cred, color.New(color.FgYellow, color.Italic))
	isTerminal := terminal.IsTerminal(int(os.Stdin.Fd()))
	reader := bufio.NewReader(os.Stdin)

	argsNr := len(args)

	if argsNr == 2 {
		if isTerminal {
			fmt.Printf("%s", console.Colorize(cred, "Enter Access Key: "))
		}
		value, _, _ := reader.ReadLine()
		accessKey = string(value)
	} else {
		accessKey = args.Get(2)
	}

	if argsNr == 2 || argsNr == 3 {
		if isTerminal {
			fmt.Printf("%s", console.Colorize(cred, "Enter Secret Key: "))
			bytePassword, _ := terminal.ReadPassword(int(os.Stdin.Fd()))
			fmt.Printf("\n")
			secretKey = string(bytePassword)
		} else {
			value, _, _ := reader.ReadLine()
			secretKey = string(value)
		}
	} else {
		secretKey = args.Get(3)
	}

	return accessKey, secretKey
}

const (
	CmdLDAPEnabled   = "CONSOLE_LDAP_ENABLED"
	StsDefaultExpire = time.Hour * 1
	StsWindowTime    = time.Minute * 10
)

func mainAliasSet(cli *cli.Context, deprecated bool) error {
	console.SetColor("AliasMessage", color.New(color.FgGreen))
	var (
		args  = cli.Args()
		alias = cleanAlias(args.Get(0))
		url   = trimTrailingSeparator(args.Get(1))
		api   = cli.String("api")
		path  = cli.String("path")
		ctype = cli.String("type")

		peerCert *x509.Certificate
		err      *probe.Error
	)

	// Support deprecated lookup flag
	if deprecated {
		lookup := strings.ToLower(strings.TrimSpace(cli.String("lookup")))
		switch lookup {
		case "", "auto":
			path = "auto"
		case "path":
			path = "on"
		case "dns":
			path = "off"
		default:
		}
	}

	switch ctype {
	case "", "auto":
		if strings.ToLower(env.Get(CmdLDAPEnabled, "off")) == "on" {
			ctype = "ldap"
		} else {
			ctype = "normal"
		}
	case "normal", "ldap":
	default:
	}

	accessKey, secretKey := fetchAliasKeys(args)
	checkAliasSetSyntax(cli, accessKey, secretKey, deprecated)

	ctx, cancelAliasAdd := context.WithCancel(globalContext)
	defer cancelAliasAdd()

	if !globalInsecure && !globalJSON && term.IsTerminal(int(os.Stdout.Fd())) {
		peerCert, err = promptTrustSelfSignedCert(ctx, url, alias)
		fatalIf(err.Trace(cli.Args()...), "Unable to initialize new alias from the provided credentials.")
	}

	var (
		stsAccessKey  string
		stsSecretKey  string
		stsSessionTk  string
		stsExpireTime time.Time
	)
	if ctype == "ldap" {
		now := time.Now()
		var e error
		stsAccessKey, stsSecretKey, stsSessionTk, e = getStsWithLDAP(url, accessKey, secretKey, peerCert)
		if e != nil {
			err = probe.NewError(e)
			fatalIf(err.Trace(cli.Args()...), "Unable to get sts AccessKey and SecretKey with provided credentials.")
			return e
		}
		stsExpireTime = now.Add(StsDefaultExpire).Add(-StsWindowTime)
	} else {
		stsAccessKey = accessKey
		stsSecretKey = secretKey
		stsSessionTk = ""
		stsExpireTime = time.Unix(0, 0)
	}

	s3Config, err := BuildS3Config(ctx, url, alias, stsAccessKey, stsSecretKey, stsSessionTk, api, path, peerCert)
	fatalIf(err.Trace(cli.Args()...), "Unable to initialize new alias from the provided credentials.")

	msg := setAlias(alias, aliasConfigV10{
		URL:          s3Config.HostURL,
		AccessKey:    accessKey,
		SecretKey:    secretKey,
		API:          s3Config.Signature,
		Path:         path,
		AType:        ctype,
		StsAccessKey: stsAccessKey,
		StsSecretKey: stsSecretKey,
		StsSessionTk: stsSessionTk,
		ExpireTime:   stsExpireTime,
	}) // Add an alias with specified credentials.

	msg.op = "set"
	if deprecated {
		msg.op = "add"
	}

	printMsg(msg)
	return nil
}

// promptTrustSelfSignedCert connects to the given endpoint and
// checks whether the peer certificate can be verified.
// If not, it computes a fingerprint of the peer certificate
// public key, asks the user to confirm the fingerprint and
// adds the peer certificate to the local trust store in the
// CAs directory.
func promptTrustSelfSignedCert(ctx context.Context, endpoint, alias string) (*x509.Certificate, *probe.Error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, probe.NewError(err)
	}

	// no need to probe certs for http endpoints.
	if req.URL.Scheme == "http" {
		return nil, nil
	}

	client := http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{
				RootCAs: globalRootCAs, // make sure to use loaded certs before probing
			},
		},
	}

	_, tlsErr := client.Do(req)
	if tlsErr == nil {
		// certs are already trusted system wide, nothing to do.
		return nil, nil
	}

	if tlsErr != nil && !strings.Contains(tlsErr.Error(), "certificate signed by unknown authority") {
		return nil, probe.NewError(tlsErr)
	}

	// Now, we fetch the peer certificate, compute the SHA-256 of
	// public key and let the user confirm the fingerprint.
	// If the user confirms, we store the peer certificate in the CAs
	// directory and retry.
	peerCert, err := fetchPeerCertificate(ctx, endpoint)
	if err != nil {
		return nil, probe.NewError(err)
	}

	// Check that the subject key id is equal to the authority key id.
	// If true, the certificate is its own issuer, and therefore, a
	// self-signed certificate.
	// Otherwise, the certificate has been issued by some other
	// certificate that is just not trusted
	if !bytes.Equal(peerCert.SubjectKeyId, peerCert.AuthorityKeyId) {
		return nil, probe.NewError(tlsErr)
	}

	fingerprint := sha256.Sum256(peerCert.RawSubjectPublicKeyInfo)
	fmt.Printf("Fingerprint of %s public key: %s\nConfirm public key y/N: ", color.GreenString(alias), color.YellowString(hex.EncodeToString(fingerprint[:])))
	answer, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return nil, probe.NewError(err)
	}
	if answer = strings.ToLower(answer); answer != "y\n" && answer != "yes\n" {
		return nil, probe.NewError(tlsErr)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: peerCert.Raw})
	if err = os.WriteFile(filepath.Join(mustGetCAsDir(), alias+".crt"), certPEM, 0o644); err != nil {
		return nil, probe.NewError(err)
	}
	return peerCert, nil
}

// fetchPeerCertificate uses the given transport to fetch the peer
// certificate from the given endpoint.
func fetchPeerCertificate(ctx context.Context, endpoint string) (*x509.Certificate, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
			},
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.TLS == nil || len(resp.TLS.PeerCertificates) == 0 {
		return nil, fmt.Errorf("Unable to read remote TLS certificate")
	}
	return resp.TLS.PeerCertificates[0], nil
}

// configurePeerCertificate adds the peer certificate to the
// TLS root CAs of s3Config. Once configured, any client
// initialized with this config trusts the given peer certificate.
func configurePeerCertificate(s3Config *Config, peerCert *x509.Certificate) {
	switch {
	case s3Config.Transport == nil:
		if globalRootCAs != nil {
			globalRootCAs.AddCert(peerCert)
		}
		s3Config.Transport = &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 15 * time.Second,
			}).DialContext,
			MaxIdleConnsPerHost:   256,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 10 * time.Second,
			DisableCompression:    true,
			TLSClientConfig:       &tls.Config{RootCAs: globalRootCAs},
		}
	case s3Config.Transport.TLSClientConfig == nil || s3Config.Transport.TLSClientConfig.RootCAs == nil:
		if globalRootCAs != nil {
			globalRootCAs.AddCert(peerCert)
		}
		s3Config.Transport.TLSClientConfig = &tls.Config{RootCAs: globalRootCAs}
	default:
		s3Config.Transport.TLSClientConfig.RootCAs.AddCert(peerCert)
	}
}

func prepareStsClient(peerCert *x509.Certificate, url string) *http.Client {
	var cas *x509.CertPool
	if globalRootCAs != nil {
		if peerCert != nil {
			globalRootCAs.AddCert(peerCert)
		}
		cas = globalRootCAs
	}
	if peerCert != nil {
		cas.AddCert(peerCert)
	}
	schem, _ := getScheme(url)
	insecure := schem != "http" && peerCert != nil
	DefaultTransport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 15 * time.Second,
		}).DialContext,
		MaxIdleConnsPerHost:   256,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 10 * time.Second,
		DisableCompression:    true,
		TLSClientConfig: &tls.Config{
			RootCAs:            cas,
			InsecureSkipVerify: insecure,
			MinVersion:         tls.VersionTLS12,
		},
	}
	c := &http.Client{
		Transport: DefaultTransport,
	}

	return c
}

func getStsWithLDAP(endpoint, ldapUser, ldapPassword string, peerCert *x509.Certificate) (stsAccessKey, stsSecretKey, stsSessionTk string, err error) {
	client := prepareStsClient(peerCert, endpoint)

	creds := credentials.New(&credentials.LDAPIdentity{
		Client:          client,
		STSEndpoint:     endpoint,
		LDAPUsername:    ldapUser,
		LDAPPassword:    ldapPassword,
		RequestedExpiry: StsDefaultExpire,
	})

	tokens, err := creds.Get()
	if err != nil {
		return "", "", "", err
	}

	return tokens.AccessKeyID, tokens.SecretAccessKey, tokens.SessionToken, nil

}
