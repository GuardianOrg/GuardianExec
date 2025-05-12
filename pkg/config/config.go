package config

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/crytic/cloudexec/pkg/log"
)

type Config struct {
	Username     string `toml:"username"`
	DigitalOcean struct {
		ApiKey          string `toml:"apiKey"`
		SpacesAccessKey string `toml:"spacesAccessKey"`
		SpacesSecretKey string `toml:"spacesSecretKey"`
		SpacesRegion    string `toml:"spacesRegion"`
	} `toml:"DigitalOcean"`
}

// Load and decrypt .env if needed
func decryptEnvIfNeeded() error {
	if _, err := os.Stat(".env"); err == nil {
		return nil // .env exists
	}

	if _, err := os.Stat(".env.enc"); err != nil {
		return fmt.Errorf("no .env or .env.enc found")
	}

	fmt.Print("🔐 Enter password to decrypt .env.enc: ")
	password, err := readPassword()
	if err != nil {
		return fmt.Errorf("failed to read password: %w", err)
	}

	cmd := exec.Command("openssl", "enc", "-aes-256-cbc", "-d",
		"-in", ".env.enc", "-out", ".env", "-pass", "pass:"+password,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		_ = os.Remove(".env") // clean up partial
		return fmt.Errorf("decryption failed: %s", strings.TrimSpace(string(output)))
	}

	fmt.Println("✅ Decrypted .env successfully")
	return nil
}

func readPassword() (string, error) {
	// Hides input if terminal supports it
	fmt.Print("(input hidden) ")
	cmd := exec.Command("stty", "-echo")
	cmd.Stdin = os.Stdin
	_ = cmd.Run()
	defer exec.Command("stty", "echo").Run()

	reader := bufio.NewReader(os.Stdin)
	pwd, err := reader.ReadString('\n')
	fmt.Println()
	return strings.TrimSpace(pwd), err
}

// Loads environment variables from a .env file
func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			os.Setenv(strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1]))
		}
	}
	return scanner.Err()
}

func Create(configValues Config) error {
	configDir := filepath.Join(os.Getenv("HOME"), ".config", "cloudexec")
	configFile := filepath.Join(configDir, "config.toml")

	err := os.MkdirAll(configDir, os.ModePerm)
	if err != nil {
		return fmt.Errorf("Failed to create configuration directory at %s: %w", configDir, err)
	}

	file, err := os.OpenFile(configFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("Failed to create configuration file at %s: %w", configFile, err)
	}
	defer file.Close()

	encoder := toml.NewEncoder(file)
	err = encoder.Encode(configValues)
	if err != nil {
		return fmt.Errorf("Failed to encode configuration values: %w", err)
	}

	log.Good("Configuration file created at: %s", configFile)
	return nil
}

func Load(configFilePath string) (Config, error) {
	var config Config

	// Step 1: Ensure env vars are loaded from decrypted .env if needed
	if err := decryptEnvIfNeeded(); err == nil {
		_ = loadDotEnv(".env")
	} // else: fallback to config file path or partial envs

	// Step 2: Read from env (can override file)
	doApiKey := os.Getenv("DIGITALOCEAN_API_KEY")
	doSpacesAccessKey := os.Getenv("DIGITALOCEAN_SPACES_ACCESS_KEY")
	doSpacesSecretKey := os.Getenv("DIGITALOCEAN_SPACES_SECRET_ACCESS_KEY")
	doSpacesRegion := os.Getenv("DIGITALOCEAN_SPACES_REGION")
	doUsername := os.Getenv("USERNAME")

	config.Username = doUsername

	if doApiKey != "" && doSpacesAccessKey != "" && doSpacesSecretKey != "" && doSpacesRegion != "" {
		config.DigitalOcean.ApiKey = doApiKey
		config.DigitalOcean.SpacesAccessKey = doSpacesAccessKey
		config.DigitalOcean.SpacesSecretKey = doSpacesSecretKey
		config.DigitalOcean.SpacesRegion = doSpacesRegion
		return config, nil
	}

	// Step 3: Fallback to file
	configFile, err := os.Open(configFilePath)
	if err != nil {
		return config, fmt.Errorf("Failed to open configuration file at %s: %w", configFilePath, err)
	}
	defer configFile.Close()

	decoder := toml.NewDecoder(configFile)
	_, err = decoder.Decode(&config)
	if err != nil {
		return config, fmt.Errorf("Failed to decode configuration file: %w", err)
	}

	// Step 4: Environment overrides again if needed
	if doApiKey != "" {
		config.DigitalOcean.ApiKey = doApiKey
	}
	if doSpacesAccessKey != "" {
		config.DigitalOcean.SpacesAccessKey = doSpacesAccessKey
	}
	if doSpacesSecretKey != "" {
		config.DigitalOcean.SpacesSecretKey = doSpacesSecretKey
	}
	if doSpacesRegion != "" {
		config.DigitalOcean.SpacesRegion = doSpacesRegion
	}

	return config, nil
}