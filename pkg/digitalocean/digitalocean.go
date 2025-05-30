package digitalocean

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/digitalocean/godo"
	"github.com/digitalocean/godo/util"

	"github.com/crytic/cloudexec/pkg/config"
	"github.com/crytic/cloudexec/pkg/log"
	"github.com/crytic/cloudexec/pkg/s3"
)

type Size struct {
	CPUs       int64
	Disk       int64
	Memory     int64
	HourlyCost float64
}

type Droplet struct {
	Name    string
	ID      int64
	IP      string
	Created string
	Size    Size
}

type Snapshot struct {
	Name string
	ID   string
}

/*
 * the vps hub, everything related to digital ocean server management
 * exports the following functions:
 * - CheckAuth(config config.Config) (string, error)
 * - CreateDroplet(config config.Config, region string, size string, userData string, jobID int64, publicKey string) (Droplet, error)
 * - GetAllDroplets(config config.Config) ([]Droplet, error)
 * - DeleteDroplet(config config.Config, dropletID int64) error
 * - GetLatestSnapshot(config config.Config) (Snapshot, error)
 */

var doClient *godo.Client
var ctx context.Context

const timeLayout = time.RFC3339
const cloudexecTag = "Purpose:cloudexec"

////////////////////////////////////////
// Internal Helper Functions

// Create and cache a godo client
func initializeDOClient(accessToken string) (*godo.Client, error) {
	// Immediately return our cached client if available
	if doClient != nil {
		return doClient, nil
	}
	doClient = godo.NewFromToken(accessToken)
	ctx = context.TODO()
	return doClient, nil
}

func createSSHKeyOnDigitalOcean(keyName string, publicKey string) (string, error) {
	createKeyRequest := &godo.KeyCreateRequest{
		Name:      keyName,
		PublicKey: publicKey,
	}
	key, _, err := doClient.Keys.Create(ctx, createKeyRequest)
	if err != nil {
		return "", fmt.Errorf("Failed to create SSH key on DigitalOcean: %w", err)
	}
	return key.Fingerprint, nil
}

// Query DigitalOcean to see if a key with the given name exists, return it's fingerprint if so
func findSSHKeyOnDigitalOcean(keyName string) (string, string, error) {
	opt := &godo.ListOptions{
		Page:    1,
		PerPage: 200, // Maximum allowed by DigitalOcean
	}
	keys, _, err := doClient.Keys.List(context.Background(), opt)
	if err != nil {
		return "", "", fmt.Errorf("Failed to list DigitalOcean SSH keys: %w", err)
	}
	for _, key := range keys {
		if key.Name == keyName {
			return key.Fingerprint, key.PublicKey, nil
		}
	}
	return "", "", fmt.Errorf("SSH key with name '%s' not found", keyName)
}

////////////////////////////////////////
// Exported Functions

// Query the droplet and spaces APIs to check whether the config contains valid credentials
func CheckAuth(config config.Config) error {
	// create a client
	doClient, err := initializeDOClient(config.DigitalOcean.ApiKey)
	if err != nil {
		return err
	}
	// Check Account authentication
	_, _, err = doClient.Account.Get(context.Background())
	if err != nil {
		return fmt.Errorf("Failed to authenticate with DigitalOcean API: %w", err)
	}
	log.Good("Successfully authenticated with DigitalOcean API")

	// Check Spaces authentication
	_, err = s3.ListBuckets(config)
	if err != nil {
		return fmt.Errorf("Failed to authenticate with DigitalOcean Spaces API: %w", err)
	}
	log.Good("Successfully authenticated with DigitalOcean Spaces API")
	return nil
}

// Launch a new droplet
func CreateDroplet(config config.Config, region string, size string, userData string, jobID int64, publicKey string) (Droplet, error) {
	var droplet Droplet
	// create a client
	doClient, err := initializeDOClient(config.DigitalOcean.ApiKey)
	if err != nil {
		return droplet, err
	}

	keyName := fmt.Sprintf("cloudexec-%v", config.Username)
	sshKeyFingerprint, savedPublicKey, err := findSSHKeyOnDigitalOcean(keyName)

	dropletName := fmt.Sprintf("%s-%v", keyName, jobID)

	if err == nil {
		if publicKey != savedPublicKey {
			return droplet, fmt.Errorf("Keys do not match! Consider removing your old key from DigitalOcean Security settings and re-running 'cloudexec launch'.")
		}
	} else {
		// Create the SSH key on DigitalOcean
		log.Wait("Saving SSH public key to DigitalOcean")
		keyName := fmt.Sprintf("cloudexec-%v", config.Username)
		sshKeyFingerprint, err = createSSHKeyOnDigitalOcean(keyName, publicKey)
		if err != nil {
			return droplet, fmt.Errorf("Failed to create SSH key on DigitalOcean: %w", err)
		}
		log.Good("SSH key is available on DigitalOcean with fingerprint: %v", sshKeyFingerprint)
	}

	snap, err := GetLatestSnapshot(config)
	if err != nil {
		return droplet, fmt.Errorf("Failed to get snapshot ID: %w", err)
	}

	// Create a new droplet
	createRequest := &godo.DropletCreateRequest{
		Name:   dropletName,
		Region: region,
		Size:   size,
		Image: godo.DropletCreateImage{
			Slug: snap.ID,
		},
		UserData: userData,
		SSHKeys: []godo.DropletCreateSSHKey{
			{
				Fingerprint: sshKeyFingerprint,
			},
		},
		Tags: []string{
			cloudexecTag,
			"Owner:" + config.Username,
			"Job:" + fmt.Sprintf("%v", jobID),
		},
		// Don't install the droplet agent
		WithDropletAgent: new(bool),
	}

	newDroplet, resp, err := doClient.Droplets.Create(ctx, createRequest)
	if err != nil {
		return droplet, fmt.Errorf("Failed to create droplet: %w", err)
	}
	var action *godo.LinkAction
	for _, a := range resp.Links.Actions {
		if a.Rel == "create" {
			action = &a
			break
		}
	}

	if action != nil {
		_ = util.WaitForActive(ctx, doClient, action.HREF)
		doDroplet, _, err := doClient.Droplets.Get(context.TODO(), newDroplet.ID)
		if err != nil {
			return droplet, fmt.Errorf("Failed to get droplet by id: %w", err)
		}
		newDroplet = doDroplet
	}

	droplet.Size = Size{
		CPUs:       int64(newDroplet.Vcpus),
		Disk:       int64(newDroplet.Disk),
		Memory:     int64(newDroplet.Memory),
		HourlyCost: float64(newDroplet.Size.PriceHourly),
	}
	droplet.Created = newDroplet.Created
	droplet.Name = newDroplet.Name
	droplet.ID = int64(newDroplet.ID)
	droplet.IP, err = newDroplet.PublicIPv4()
	if err != nil {
		return droplet, fmt.Errorf("Failed to get droplet IP: %w", err)
	}

	return droplet, nil
}

func GetDropletById(config config.Config, id int64) (Droplet, error) {
	// create a client
	doClient, err := initializeDOClient(config.DigitalOcean.ApiKey)
	if err != nil {
		return Droplet{}, err
	}

	dropletInfo, _, err := doClient.Droplets.Get(context.TODO(), int(id))
	if err != nil {
		return Droplet{}, fmt.Errorf("Failed to get droplet by id: %v", err)
	}
	pubIp, err := dropletInfo.PublicIPv4()
	if err != nil {
		return Droplet{}, fmt.Errorf("Failed to fetch droplet IP: %w", err)
	}

	return Droplet{
		Name:    dropletInfo.Name,
		ID:      int64(dropletInfo.ID),
		IP:      pubIp,
		Created: dropletInfo.Created,
		Size: Size{
			CPUs:       int64(dropletInfo.Vcpus),
			Disk:       int64(dropletInfo.Disk),
			Memory:     int64(dropletInfo.Memory),
			HourlyCost: float64(dropletInfo.Size.PriceHourly),
		},
	}, nil
}

// GetAllDroplets returns a list of droplets with the given tag using a godo client
func GetAllDroplets(config config.Config) ([]Droplet, error) {
	var droplets []Droplet
	// create a client
	doClient, err := initializeDOClient(config.DigitalOcean.ApiKey)
	if err != nil {
		return droplets, err
	}
	targetTag := fmt.Sprintf("Owner:%s", config.Username)

	opts := &godo.ListOptions{}

	for { // loop through all pages of the droplet list
		myDroplets, resp, err := doClient.Droplets.ListByTag(ctx, targetTag, opts)
		if err != nil {
			return droplets, fmt.Errorf("Failed to fetch droplets by name: %w", err)
		}

		for _, droplet := range myDroplets {
			for _, tag := range droplet.Tags {
				if tag != cloudexecTag { // don't do anything until we find a cloudexec tag
					continue
				}
				pubIp, err := droplet.PublicIPv4()
				if err != nil {
					return droplets, fmt.Errorf("Failed to fetch droplet IP: %w", err)
				}
				droplets = append(droplets, Droplet{
					Name:    droplet.Name,
					ID:      int64(droplet.ID),
					IP:      pubIp,
					Created: droplet.Created,
					Size: Size{
						CPUs:       int64(droplet.Vcpus),
						Disk:       int64(droplet.Disk),
						Memory:     int64(droplet.Memory),
						HourlyCost: float64(droplet.Size.PriceHourly),
					},
				})
				break
			}
		}

		if resp.Links == nil || resp.Links.IsLastPage() {
			break
		}

		page, err := resp.Links.CurrentPage()
		if err != nil {
			return droplets, fmt.Errorf("Failed to fetch page of droplets list: %w", err)
		}

		opts.Page = page + 1
	}

	return droplets, nil
}

func DeleteDroplet(config config.Config, dropletID int64) error {
	// create a client
	doClient, err := initializeDOClient(config.DigitalOcean.ApiKey)
	if err != nil {
		return err
	}
	_, err = doClient.Droplets.Delete(context.Background(), int(dropletID))
	if err != nil {
		return fmt.Errorf("Failed to delete droplet: %w", err)
	}
	return nil
}

func GetLatestSnapshot(config config.Config) (Snapshot, error) {
	empty := Snapshot{
		ID:   "",
		Name: "",
	}
	// create a client
	doClient, err := initializeDOClient(config.DigitalOcean.ApiKey)
	if err != nil {
		return empty, err
	}

	var latestSnapshot *godo.Snapshot
	var latestCreatedAt time.Time

	options := &godo.ListOptions{
		Page:    1,
		PerPage: 50,
	}

	for {
		snapshots, resp, err := doClient.Snapshots.ListDroplet(context.Background(), options)
		if err != nil {
			return empty, fmt.Errorf("Failed to list snapshots: %w", err)
		}
		for _, snapshot := range snapshots {
			snapshotCreatedAt, err := time.Parse(timeLayout, snapshot.Created)
			if err != nil {
				return empty, fmt.Errorf("Failed to parse snapshot creation timestamp: %w", err)
			}
			if (latestSnapshot == nil || snapshotCreatedAt.After(latestCreatedAt)) && strings.HasPrefix(snapshot.Name, "cloudexec-") {
				latestSnapshot = &snapshot
				latestCreatedAt = snapshotCreatedAt
			}
		}

		if resp.Links == nil || resp.Links.IsLastPage() {
			break
		}

		options.Page++
	}

	if latestSnapshot == nil {
		return Snapshot{
			ID:   "ubuntu-20-04-x64",
			Name: "fallback",
		}, nil
	}

	return Snapshot{
		ID:   latestSnapshot.ID,
		Name: latestSnapshot.Name,
	}, nil
}
