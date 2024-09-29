package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/jhaals/yopass/pkg/yopass"
	"github.com/spf13/pflag"
	"github.com/spf13/viper"
)

const usageTemplate = `
Yopass - Secure sharing for secrets, passwords and files

Flags:
%s

Settings are read from flags, environment variables, or a config file located at
~/.config/yopass/defaults.<json,toml,yml,hcl,ini,...> in this order. Environment
variables have to be prefixed with YOPASS_ and dashes become underscores.

Examples:
      # Open $EDITOR to encrypt and share data written. Will fallback to vi if unset.
      yopass

      # Encrypt and share secret from stdin
      printf 'secret message' | yopass

      # Encrypt and share secret file
      yopass --file /path/to/secret.conf

      # Share secret multiple time a whole day
      cat secret-notes.md | yopass --expiration=1d --one-time=false

      # Decrypt secret to stdout
      yopass --decrypt https://yopass.se/#/...

Website: %s
`

func init() {
	// Defaults
	viper.SetDefault("api", "https://api.yopass.se")
	viper.SetDefault("url", "https://yopass.se")
	viper.SetDefault("one-time", true)
	viper.SetDefault("expiration", "1h")

	// Config file
	viper.SetConfigName("defaults")
	viper.AddConfigPath("$HOME/.config/yopass")
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			fmt.Fprintln(os.Stderr, "Yopass config file invalid:", err)
			os.Exit(3)
		}
	}

	// Environment variables
	viper.SetEnvPrefix("yopass")
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_"))
	viper.AutomaticEnv()

	// Command-line flags
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ContinueOnError)
	pflag.String("api", viper.GetString("api"), "Yopass API server location")
	pflag.String("decrypt", viper.GetString("decrypt"), "Decrypt secret URL")
	pflag.String("expiration", viper.GetString("expiration"), "Duration after which secret will be deleted [1h, 1d, 1w]")
	pflag.String("file", viper.GetString("file"), "Read secret from file instead of stdin")
	pflag.String("key", viper.GetString("key"), "Manual encryption/decryption key")
	pflag.Bool("one-time", viper.GetBool("one-time"), "One-time download")
	pflag.String("url", viper.GetString("url"), "Yopass public URL")
	viper.BindPFlags(pflag.CommandLine)
}

func main() {
	if code := parse(os.Args[1:], os.Stderr); code >= 0 {
		os.Exit(code)
	}

	info, err := os.Stdin.Stat()
	if err != nil {
		fmt.Println("Failed to get file info:", err)
		os.Exit(1)
	}

	if viper.IsSet("decrypt") {
		err = decrypt(os.Stdout)
	} else if viper.IsSet("file") {
		err = encryptFileByName(viper.GetString("file"), os.Stdout)
	} else if info.Mode()&os.ModeCharDevice != 0 {
		err = encryptEditor()
	} else {
		err = encrypt(os.Stdin, os.Stdout)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func decrypt(out io.Writer) error {
	if !strings.HasPrefix(viper.GetString("decrypt"), viper.GetString("url")) {
		return fmt.Errorf("Unconfigured yopass decrypt URL, set --api and --url")
	}

	id, key, _, keyOpt, err := yopass.ParseURL(viper.GetString("decrypt"))
	if err != nil {
		return fmt.Errorf("Invalid yopass decrypt URL: %w", err)
	}

	if keyOpt || key == "" {
		if !viper.IsSet("key") {
			return fmt.Errorf("Manual decryption key required, set --key")
		}
		key = viper.GetString("key")
	}

	msg, err := yopass.Fetch(viper.GetString("api"), id)
	if err != nil {
		return fmt.Errorf("Failed to fetch secret: %w", err)
	}

	pt, _, err := yopass.Decrypt(strings.NewReader(msg), key)
	if err != nil {
		return fmt.Errorf("Failed to decrypt secret: %w", err)
	}

	// Note yopass decrypt currently always prints the content to stdout. This
	// could be changed to create a file, but will need to handle the case that
	// the file already exists.
	_, err = fmt.Fprint(out, pt)
	return err
}

func encryptEditor() error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("Failed to open /dev/tty: %v", err)
	}

	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}

	tmpFile, err := os.CreateTemp("", "yopasstmp*.txt")
	if err != nil {
		return fmt.Errorf("Failed to create temporary file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	cmd := exec.Command(editor, tmpFile.Name())
	cmd.Stdin = tty
	cmd.Stdout = tty
	cmd.Stderr = tty

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("Failed running editor: %w", err)
	}

	content, err := os.ReadFile(tmpFile.Name())
	if err != nil {
		return fmt.Errorf("Failed reading temporary file: %w", err)
	}

	// Exit without error if content is empty
	if string(content) == "" {
		return nil
	}

	stringReader := strings.NewReader(string(content))
	stringReadCloser := io.NopCloser(stringReader)
	return encrypt(stringReadCloser, os.Stdout)
}

func encryptFileByName(filename string, out io.Writer) error {
	var in, err = os.Open(filename)
	defer in.Close()
	if err != nil {
		return fmt.Errorf("Failed to open file: %w", err)
	}
	return encrypt(in, out)
}

func encryptStdin(in *os.File, out io.Writer) error {
	return encrypt(in, out)
}

func encrypt(in io.ReadCloser, out io.Writer) error {
	exp := expiration(viper.GetString("expiration"))
	if exp == 0 {
		return fmt.Errorf("Expiration can only be 1 hour (1h), 1 day (1d), or 1 week (1w)")
	}

	key, err := encryptionKey(viper.GetString("key"))
	if err != nil {
		return fmt.Errorf("Failed to generate encryption key: %w", err)
	}

	msg, err := yopass.Encrypt(in, key)
	if err != nil {
		return fmt.Errorf("Failed to encrypt secret: %w", err)
	}

	id, err := yopass.Store(viper.GetString("api"), yopass.Secret{
		Expiration: exp,
		Message:    msg,
		OneTime:    viper.GetBool("one-time"),
	})
	if err != nil {
		return fmt.Errorf("Failed to store secret: %w", err)
	}

	url := viper.GetString("url")
	_, err = fmt.Fprintln(out, yopass.SecretURL(url, id, key, viper.IsSet("file"), viper.IsSet("key")))
	return err
}

func encryptionKey(key string) (string, error) {
	if key != "" {
		return key, nil
	}
	return yopass.GenerateKey()
}

func expiration(s string) int32 {
	switch s {
	case "1h":
		return 3600
	case "1d":
		return 3600 * 24
	case "1w":
		return 3600 * 24 * 7
	default:
		return 0
	}
}

func parse(args []string, stderr io.Writer) int {
	pflag.Usage = func() {
		fmt.Fprintf(
			stderr,
			strings.TrimPrefix(usageTemplate, "\n"),
			strings.TrimSuffix(pflag.CommandLine.FlagUsages(), "\n"),
			viper.Get("url"),
		)
	}

	if err := pflag.CommandLine.Parse(args); err != nil {
		if err == pflag.ErrHelp {
			return 0
		}
		fmt.Fprintln(stderr, err)
		return 1
	}
	return -1
}
