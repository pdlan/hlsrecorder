package cmd

import (
	"bytes"
	"io"
	"os"
	"time"
	"net"
	"net/http"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/rand"
	"encoding/json"
	"encoding/hex"

	"github.com/spf13/cobra"
	"github.com/AdguardTeam/golibs/log"
	"github.com/AdguardTeam/gomitmproxy"
	"github.com/AdguardTeam/gomitmproxy/mitm"
	"github.com/AdguardTeam/gomitmproxy/proxyutil"

	"hlsrecorder/request"
)

var metadataFile *os.File
var metadataEncoder *json.Encoder
var fileDir string

func record(cmd *cobra.Command, args []string) {
	key, _ := cmd.Flags().GetString("key")
	crt, _ := cmd.Flags().GetString("crt")
	fileDir, _ = cmd.Flags().GetString("filedir")
	metadata, _ := cmd.Flags().GetString("metadata")
	listen, _ := cmd.Flags().GetString("listen")
	username, _ := cmd.Flags().GetString("username")
	password, _ := cmd.Flags().GetString("password")

	os.Mkdir(fileDir, 0755)

	metadataFile, err := os.OpenFile(metadata, os.O_APPEND | os.O_WRONLY | os.O_CREATE, 0600)
	if err != nil {
		log.Fatal(err)
	}
	metadataEncoder = json.NewEncoder(metadataFile)

	tlsCert, err := tls.LoadX509KeyPair(crt, key)
	if err != nil {
		log.Fatal(err)
	}
	privateKey := tlsCert.PrivateKey.(*rsa.PrivateKey)

	x509c, err := x509.ParseCertificate(tlsCert.Certificate[0])
	if err != nil {
		log.Fatal(err)
	}

	mitmConfig, err := mitm.NewConfig(x509c, privateKey, nil)

	if err != nil {
		log.Fatal(err)
	}

	// Generate certs valid for 7 days.
	mitmConfig.SetValidity(time.Hour * 24 * 7)
	// Set certs organization.
	mitmConfig.SetOrganization("gomitmproxy")

	listener, err := net.Listen("tcp", listen)
	
	if err != nil {
		log.Fatal(err)
	}

	proxy := gomitmproxy.NewProxy(gomitmproxy.Config{
		APIHost:	"gomitmproxy",
		MITMConfig:	mitmConfig,
		OnResponse:	onResponse,
		Username: username,
		Password: password,
	})

	proxy.Serve(listener)
}

func randomHex(n int) string {
	bytes := make([]byte, n)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func onResponse(session *gomitmproxy.Session) *http.Response {
	res := session.Response()
	req := session.Request()
	uri := req.URL.String()
	log.Printf("onResponse: %s", uri)

	body, err := io.ReadAll(res.Body)
	res.Body.Close()
	if err != nil {
		return proxyutil.NewErrorResponse(req, err)
	}
	res.Body = io.NopCloser(bytes.NewReader(body))
	id := randomHex(16)
	metadata := request.Metadata {
		Host: req.Host,
		URI: uri,
		Time: time.Now().UnixMicro(),
		Id: id,
		Status: res.StatusCode,
		Location: res.Header.Get("Location"),
	}
	filename := fileDir + "/" + id
	err = os.WriteFile(filename, body, 0644)
	if err != nil {
		log.Printf("Warning: failed to save %s: %s", uri, err)
	}
	metadataEncoder.Encode(metadata)
	return res
}

// recordCmd represents the record command
var recordCmd = &cobra.Command{
	Use:   "record",
	Short: "Record HLS live streaming with MITM",
	Run: record,
}

func init() {
	rootCmd.AddCommand(recordCmd)

	// Here you will define your flags and configuration settings.

	// Cobra supports Persistent Flags which will work for this command
	// and all subcommands, e.g.:
	// recordCmd.PersistentFlags().String("foo", "", "A help for foo")

	// Cobra supports local flags which will only run when this command
	// is called directly, e.g.:
	// recordCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
	recordCmd.Flags().String("username", "", "Proxy username")
	recordCmd.Flags().String("password", "", "Proxy password")
}
