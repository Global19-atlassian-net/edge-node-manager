package process

import (
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/fredli74/lockfile"
	"github.com/resin-io/edge-node-manager/application"
	"github.com/resin-io/edge-node-manager/config"
	deviceStatus "github.com/resin-io/edge-node-manager/device/status"
	processStatus "github.com/resin-io/edge-node-manager/process/status"
	"github.com/resin-io/edge-node-manager/radio/bluetooth"
)

var (
	delay          time.Duration
	CurrentStatus  processStatus.Status
	TargetStatus   processStatus.Status
	UpdatesPending bool
	Lock           *lockfile.LockFile
)

// Run processes the application, checking for new commits, provisioning and updating devices
func Run(a *application.Application) []error {
	log.Info("----------------------------------------------------------------------------------------------------")

	// Put all provisioned devices associated with this application
	defer a.PutDevices()

	// Pause the process if necessary
	if err := pause(); err != nil {
		return []error{err}
	}

	// Validate application to ensure the board type has been set
	if a.BoardType == "" {
		log.WithFields(log.Fields{
			"Application": a.Name,
			"Error":       "Application board type not set",
		}).Warn("Processing application")
		return nil
	}

	// Handle delete flag
	if err := a.HandleDeleteFlag(); err != nil {
		return []error{err}
	}

	// Print application info
	log.WithFields(log.Fields{
		"Application":       a.Name,
		"Number of devices": len(a.Devices),
	}).Info("Processing application")

	// Reset the bluetooth device to clean up any left over go routines etc. Quick fix
	if err := bluetooth.ResetDevice(); err != nil {
		return []error{err}
	}

	// Get all online devices associated with this application
	if err := a.GetOnlineDevices(); err != nil {
		return []error{err}
	}

	// Provision non-provisoned online devices associated with this application
	if errs := a.ProvisionDevices(); errs != nil {
		return errs
	}

	// Set the status of all offline provisioned devices associated with this application to OFFLINE
	if errs := a.SetOfflineDeviceStatus(); errs != nil {
		return errs
	}

	// Update firmware for all online devices associated with this application
	if errs := a.UpdateOnlineDevices(); errs != nil {
		return errs
	}

	// Update config for all online devices associated with this application
	if errs := a.UpdateConfigOnlineDevices(); errs != nil {
		return errs
	}

	// Update environment for all online devices associated with this application
	if errs := a.UpdateEnvironmentOnlineDevices(); errs != nil {
		return errs
	}

	// Handle device flags
	if err := a.HandleFlags(); err != nil {
		return []error{err}
	}

	return nil
}

func Pending() {
	for _, a := range application.List {
		for _, d := range a.Devices {
			if d.Commit != d.TargetCommit && d.Status != deviceStatus.OFFLINE {
				UpdatesPending = true
				return
			}
		}
	}

	UpdatesPending = false
	return
}

func init() {
	log.SetLevel(config.GetLogLevel())

	var err error
	if delay, err = config.GetPauseDelay(); err != nil {
		log.WithFields(log.Fields{
			"Error": err,
		}).Fatal("Unable to load pause delay")
	}

	if err = lockContainerUpdates(); err != nil {
		log.WithFields(log.Fields{
			"Error": err,
		}).Fatal("Unable to lock container updates")
	}

	CurrentStatus = processStatus.RUNNING
	TargetStatus = processStatus.RUNNING
	UpdatesPending = false

	log.WithFields(log.Fields{
		"Pause delay": delay,
	}).Debug("Initialise process")
}

func lockContainerUpdates() error {
	lockFileLocation := config.GetLockFileLocation()

	var err error
	Lock, err = lockfile.Lock(lockFileLocation)
	return err
}

func pause() error {
	if TargetStatus != processStatus.PAUSED {
		return nil
	}

	if err := bluetooth.CloseDevice(); err != nil {
		return err
	}

	Lock.Unlock()

	CurrentStatus = processStatus.PAUSED
	log.WithFields(log.Fields{
		"Status": CurrentStatus,
	}).Info("Process status")

	for TargetStatus == processStatus.PAUSED {
		time.Sleep(delay)
	}

	if err := bluetooth.OpenDevice(); err != nil {
		return err
	}

	if err := lockContainerUpdates(); err != nil {
		return err
	}

	CurrentStatus = processStatus.RUNNING
	log.WithFields(log.Fields{
		"Status": CurrentStatus,
	}).Info("Process status")

	return nil
}
