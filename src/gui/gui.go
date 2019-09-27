package gui

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"time"

	astilectron "github.com/asticode/go-astilectron"
	bootstrap "github.com/asticode/go-astilectron-bootstrap"
	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/furiousteam/BLOC-GUI-Miner/src/gui/miner"
)

// GUI implements the core control for the GUI miner
type GUI struct {
	// window is the main Astilectron window
	window *astilectron.Window
	// astilectronOptions holds the Astilectron options
	astilectronOptions bootstrap.Options
	// config for the miner
	config *Config
	// this set the miner in debug mode
	inDebugMode bool
	// miner is the selected miner backend as chosen by the user
	miner miner.Miner
	// logger logs to stdout
	logger *logrus.Entry
	// workingDir holds the current working directory
	workingDir string
	// currentHashrate of the user if mining
	lastHashrate float64
	// miningStatsTicker controls the interval for fetching mining stats from
	// the selected miner
	miningStatsTicker *time.Ticker
	// networkStatsTicker controls the interval for fetching network, trading
	// and other stats
	networkStatsTicker *time.Ticker
}

// New creates a new instance of the miner application
func New(
	appName string,
	config *Config,
	asset bootstrap.Asset,
	restoreAssets bootstrap.RestoreAssets,
	apiEndpoint string,
	coinType string,
	coinAlgo string,
	XmrigAlgo string,
	XmrigVariant string,
	workingDir string,
	isDebug bool) (*GUI, error) {

	if apiEndpoint == "" {
		return nil, errors.New("The API Endpoint must be specified")
	}

	gui := GUI{
		config:       config,
		workingDir:   workingDir,
		inDebugMode:  isDebug,
	}

	// If no config is specified then this is the first run
	startPage := "firstrun.html"
	if gui.config != nil {
		startPage = "index.html"
		// Already configured, set up the miner
		var err error
		gui.config.Miner.HardwareType = gui.config.HardwareType // copy the HardwareType from config to miner
		gui.miner, err = miner.CreateMiner(gui.config.Miner)
		if err != nil {
			return nil,
				fmt.Errorf("Unable to use '%s' as miner: %s", gui.config.Miner.Type, err)
		}
	} else {
		// Nothing has been configured yet, set some defaults
		gui.config = &Config{
			APIEndpoint:  apiEndpoint,
			CoinType:     coinType,
			CoinAlgo:     coinAlgo,
			XmrigAlgo:    XmrigAlgo,
			XmrigVariant: XmrigVariant,
			HardwareType: 1,
			Mid:          uuid.New().String(),
		}
	}
	var menu []*astilectron.MenuItemOptions

	// Setup the logging, by default we log to stdout
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp:   true,
		TimestampFormat: "Jan 02 15:04:05",
	})
	logrus.SetLevel(logrus.InfoLevel)

	logrus.SetOutput(os.Stdout)

	// Create the window options
	var winHeight int
	if gui.inDebugMode {
		winHeight = 1000
	} else {
		winHeight = 780
	}
	windowOptions := astilectron.WindowOptions{
		Frame:           astilectron.PtrBool(true),
		BackgroundColor: astilectron.PtrStr("#001B45"),
		Center:          astilectron.PtrBool(true),
		Height:          astilectron.PtrInt(winHeight),
		MinHeight:       astilectron.PtrInt(500),
		Width:           astilectron.PtrInt(1220),
		MinWidth:        astilectron.PtrInt(1220),
		MaxWidth:        astilectron.PtrInt(1500),
	}

	if gui.inDebugMode {
		logrus.SetLevel(logrus.DebugLevel)
		debugLog, err := os.OpenFile(
			filepath.Join(gui.workingDir, "debug.log"),
			os.O_CREATE|os.O_TRUNC|os.O_WRONLY,
			0644)
		if err != nil {
			panic(err)
		}
		// TODO: logrus.SetOutput(debugLog)
		_ = debugLog

		// We only show the menu bar in debug mode
		menu = append(menu, &astilectron.MenuItemOptions{
			Label: astilectron.PtrStr("File"),
			SubMenu: []*astilectron.MenuItemOptions{
				{
					Role: astilectron.MenuItemRoleClose,
				},
			},
		})
	}
	// To make copy and paste work on Mac, the copy and paste entries need to
	// be defined, the alternative is to implement the clipboard API
	// https://github.com/electron/electron/blob/master/docs/api/clipboard.md
	if runtime.GOOS == "darwin" {
		menu = append(menu, &astilectron.MenuItemOptions{
			Label: astilectron.PtrStr("Edit"),
			SubMenu: []*astilectron.MenuItemOptions{
				{
					Role: astilectron.MenuItemRoleCut,
				},
				{
					Role: astilectron.MenuItemRoleCopy,
				},
				{
					Role: astilectron.MenuItemRolePaste,
				},
				{
					Role: astilectron.MenuItemRoleSelectAll,
				},
			},
		})

		windowOptions.Frame = astilectron.PtrBool(gui.inDebugMode)
		windowOptions.TitleBarStyle = astilectron.PtrStr("hidden")
	}

	// Setting the WithFields now will ensure all log entries from this point
	// includes the fields
	gui.logger = logrus.WithFields(logrus.Fields{
		"service": "bloc-gui-miner",
	})

	gui.astilectronOptions = bootstrap.Options{
		Debug:         gui.inDebugMode,
		Asset:         asset,
		RestoreAssets: restoreAssets,
		Windows: []*bootstrap.Window{{
			Homepage:       startPage,
			MessageHandler: gui.handleElectronCommands,
			Options:        &windowOptions,
		}},
		AstilectronOptions: astilectron.Options{
			AppName:            appName,
			AppIconDarwinPath:  "resources/icon-bloc.icns",
			AppIconDefaultPath: "resources/icon-bloc.png",
		},
		// TODO: Fix this tray to display nicely
		/*TrayOptions: &astilectron.TrayOptions{
			Image:   astilectron.PtrStr("/static/i/miner-logo.png"),
			Tooltip: astilectron.PtrStr(appName),
		},*/
		MenuOptions: menu,
		// OnWait is triggered as soon as the electron window is ready and running
		OnWait: func(
			_ *astilectron.Astilectron,
			windows []*astilectron.Window,
			_ *astilectron.Menu,
			_ *astilectron.Tray,
			_ *astilectron.Menu) error {
			gui.window = windows[0]
			gui.miningStatsTicker = time.NewTicker(time.Second * 5)
			gui.logger.Info("Start capturing mining stats")
			go gui.updateMiningStatsLoop()
			gui.networkStatsTicker = time.NewTicker(time.Second * 20)
			go func() {
				for _ = range gui.networkStatsTicker.C {
					gui.updateNetworkStats()
				}
			}()
			// Trigger a network stats update as soon as we start
			gui.updateNetworkStats()
			// uncomment this to have development tools opened when the app is built
			if gui.inDebugMode {
				gui.window.OpenDevTools()
			}
			return nil
		},
	}

	gui.logger.Info("Setup complete")
	return &gui, nil
}

// Run the miner!
func (gui *GUI) Run() error {
	gui.logger.Info("Starting miner")
	err := bootstrap.Run(gui.astilectronOptions)
	if err != nil {
		return err
	}
	err = gui.stopMiner()
	if err != nil {
		return err
	}
	gui.miningStatsTicker.Stop()
	gui.networkStatsTicker.Stop()
	return nil
}

// handleElectronCommands handles the messages sent by the Electron front-end
func (gui *GUI) handleElectronCommands(
	_ *astilectron.Window,
	command bootstrap.MessageIn) (interface{}, error) {

	gui.logger.WithField(
		"command", command.Name,
	).Debug("Received command from Electron")

	// Every Electron command has a name together with a payload containing the
	// actual message
	switch command.Name {

	// get-username is received on the first run of the miner
	case "get-username":
		var username string
		currentUser, err := user.Current()
		if err == nil {
			if currentUser.Name != "" {
				username = currentUser.Name
			} else if currentUser.Username != "" {
				username = currentUser.Username
			}
		}
		return username, nil

	// get-miner-path is requested so the UI can show the path to exclude
	// in antivirus software
	case "get-miner-path":
		return filepath.Join(gui.workingDir, "miner"), nil

	// get-miner-type is requested so the UI can show only the coins
	// that can be mined by a specific minining software
	case "get-miner-type":
		scanPath := filepath.Join(gui.workingDir, "miner")
		minerType, _, err := miner.DetermineMinerType(scanPath)

		if err == nil {
			return minerType, nil
		} else {
			return "", nil
		}

	// get-pools-list requests the recommended pool list from the miner API
	// and returns the rendered HTML
	case "get-pools-list":
		var newConfig frontendConfig
		err := json.Unmarshal(command.Payload, &newConfig)
		if err != nil {
			_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
				Data: fmt.Sprintf("Unable to fetch pool list from API."+
					"Internal error."+
					"<br/>The error was '%s'", err),
			})
			gui.logger.Fatalf("Unable to fetch pool list: '%s'", err)
		}

		gui.config.CoinType = newConfig.CoinType

		// Grab the pool list and send that to the GUI as well
		poolJSONs, err := gui.GetPoolList()
		if err != nil {
			_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
				Data: fmt.Sprintf("Unable to fetch pool list from API."+
					"Please check that you are connected to the internet and try again."+
					"<br/>The error was '%s'", err),
			})
			// Give the UI some time to display the message
			time.Sleep(time.Second * 15)
			gui.logger.Fatalf("Unable to fetch pool list: '%s'", err)
		}
		poolTemplate, err := gui.GetPoolTemplate()
		if err != nil {
			log.Fatalf("Unable to load pool template: '%s'", err)
		}
		var poolsList string
		for i, poolData := range poolJSONs {
			var templateHTML bytes.Buffer
			err = poolTemplate.Execute(&templateHTML, poolData)
			if err != nil {
				log.Fatalf("Unable to load pool template: '%s'", err)
			}
			// TODO: This is a dirty way to only show the top 3 and reveal the rest
			// when needed. An API that implements paging is needed to fix this
			if i == 3 {
				// poolsList += "<a href=\"#\" id=\"show_pool_list\">Show all</a>"
				// poolsList += "<div id=\"pool_list_bottom\" class=\"dn\">"
			}
			poolsList += templateHTML.String()
		}
		// TODO: Part of the hack above
		poolsList += "</div>"
		return poolsList, nil

	// get-processing-config returns the current miner's processing config
	case "get-processing-config":
		if gui.miner == nil {
			_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
				Data: fmt.Sprintf("Unable to fetch miner config." +
					"Please check that your miner is working and running."),
			})
			return "", nil
		}
		// Call the stats method to get processing info first, this causes the
		// stats to be cached by the miner
		_, _ = gui.miner.GetStats()
		processingConfig := gui.miner.GetProcessingConfig()
		configBytes, err := json.Marshal(processingConfig)
		if err != nil {
			_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
				Data: fmt.Sprintf("Unable to fetch miner config."+
					"Please check that your miner is working and running."+
					"<br/>The error was '%s'", err),
			})
		}
		return string(configBytes), nil

	// get-coins-content is called to get the content for different coins
	case "get-coins-content":
		gui.logger.Info("Getting coins content.json")

		dataBytes, err := gui.GetCoinContentJson()
		if err != nil {
			gui.logger.Errorf("Unable to get coins content.json: %s", err)
			return "", nil
		}
		return string(dataBytes), nil

	// configure is sent after the firstrun setup has been completed
	case "save-configuration":
		// HACK: Adding a slight delay before switching to the mining dashboard
		// after initial setup to have the user at least see the 'configure' message
		time.Sleep(time.Second * 3)
		gui.configureMiner(command)
		return "Ok", nil

	// reconfigure is sent after settings are changes by the user
	// NOTE: this function is no longer used, as the miner webpage gets reloaded (instead of calling reconfigure) when it's settings change
	/*
	case "reconfigure":
		gui.logger.Info("Reconfiguring miner")
		err := gui.stopMiner()
		if err != nil {
			_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
				Data: fmt.Sprintf("Unable to stop miner for reconfigure."+
					"Please close the miner and open it again."+
					"<br/>The error was '%s'", err),
			})
			// Give the UI some time to display the message
			time.Sleep(time.Second * 15)
			gui.logger.Fatalf("Unable to reconfigure miner: '%s'", err)
		}
		gui.logger.WithField(
			"name", command.Name,
		).Debug("Received command from Electrom")
		gui.configureMiner(command)
		// Fake some time to have GUI at least display the message
		time.Sleep(time.Second * 3)
		gui.startMiner()
		gui.logger.Info("Miner reconfigured")

		gui.lastHashrate = 0.00
		// Trigger pool update
		go gui.updateNetworkStats()

		return "Ok", nil
	*/

	// get-config-file is sent before any other command from the index.html
	case "get-config-file":
		gui.logger.Info("Sending config to frontend")
		currentConfig := frontendConfig{
			CoinType:     gui.config.CoinType,
			CoinAlgo:     gui.config.CoinAlgo,
			XmrigAlgo:    gui.config.XmrigAlgo,
			XmrigVariant: gui.config.XmrigVariant,
			HardwareType: gui.config.HardwareType,
		}

		dataBytes, err := json.Marshal(currentConfig)
		if err != nil {
			gui.logger.Errorf("Unable to send config to front-end: %s", err)
			return "", nil
		}
		return string(dataBytes), nil

	// start-miner is sent after configuration or when the user
	// clicks 'start mining'
	case "start-miner":
		gui.startMiner()

	// stop-miner is sent whenever the user clicks 'stop mining'
	case "stop-miner":
		err := gui.stopMiner()
		if err != nil {
			_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
				Data: fmt.Sprintf("Unable to stop miner backend."+
					"Please close the miner and open it again."+
					"<br/>The error was '%s'", err),
			})
			// Give the UI some time to display the message
			time.Sleep(time.Second * 15)
			gui.logger.Fatalf("Unable to stop the miner: '%s'", err)
		}
	}
	return nil, fmt.Errorf("'%s' is an unknown command", command.Name)
}

// configureMiner creates the miner configuration to use
func (gui *GUI) configureMiner(command bootstrap.MessageIn) {
	gui.logger.Info("Configuring miner")

	var newConfig frontendConfig
	err := json.Unmarshal(command.Payload, &newConfig)
	if err != nil {
		_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
			Data: fmt.Sprintf("Unable to configure miner."+
				"Please check your configuration is valid."+
				"<br/>The error was '%s'", err),
		})
		// Give the UI some time to display the message
		time.Sleep(time.Second * 15)
		gui.logger.Fatalf("Unable to configure miner: '%s'", err)
	}
	// gui.logger.Info(fmt.Printf("%+v\n", newConfig))
	gui.config.Address = newConfig.Address
	gui.config.PoolID = newConfig.Pool
	gui.config.CoinType = newConfig.CoinType
	gui.config.CoinAlgo = newConfig.CoinAlgo
	gui.config.XmrigAlgo = newConfig.XmrigAlgo
	gui.config.XmrigVariant = newConfig.XmrigVariant
	gui.config.HardwareType = newConfig.HardwareType

	scanPath := filepath.Join(gui.workingDir, "miner")
	// TODO: Fix own miner paths option
	/*if gui.config.Miner.Path != "" {
		//scanPath = path.Base(gui.config.Miner.Path)
	}*/
	gui.logger.WithField(
		"scan_path", scanPath,
	).Debug("Determining miner type")

	// Determine the type of miner bundled
	minerType, minerPath, err := miner.DetermineMinerType(scanPath)
	if err != nil {
		_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
			Data: fmt.Sprintf("Unable to configure miner."+
				"Could not determine the miner type."+
				"<br/>The error was '%s'", err),
		})
		// Give the UI some time to display the message
		time.Sleep(time.Second * 15)
		gui.logger.Fatalf("Unable to configure miner: '%s'", err)
	}

	// Write config for this miner
	gui.config.Miner = miner.Config{
		Type:         minerType,
		Path:         minerPath,
		HardwareType: gui.config.HardwareType,
	}
	gui.logger.WithFields(logrus.Fields{
		"path": minerPath,
		"type": minerType,
	}).Debug("Creating miner")
	gui.miner, err = miner.CreateMiner(gui.config.Miner)
	if err != nil {
		_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
			Data: fmt.Sprintf("Unable to configure miner."+
				"<br/>The error was '%s'", err),
		})
		// Give the UI some time to display the message
		time.Sleep(time.Second * 15)
		gui.logger.Fatalf("Unable to configure miner: '%s'", err)
	}

	// The pool API returns the low-end hardware host:port config for pool
	gui.logger.Debug("Getting pool information")
	poolInfo, err := gui.GetPool(gui.config.PoolID)
	if err != nil {
		_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
			Data: fmt.Sprintf("Unable to configure miner."+
				"Please check that you are connected to the internet."+
				"<br/>The error was '%s'", err),
		})
		// Give the UI some time to display the message
		time.Sleep(time.Second * 15)
		gui.logger.Fatalf("Unable to configure miner: '%s'", err)
	}

	// Write the config for the specified miner
	gui.logger.Debug("Writing miner config")

	var poolAddress string
	if gui.config.HardwareType == 1 {
		poolAddress = poolInfo.MiningPorts.Cpu // CPU mining
	} else if gui.config.HardwareType == 2 {
		poolAddress = poolInfo.MiningPorts.Gpu // GPU mining
	} else {
		poolAddress = poolInfo.Config // if HardwareType failed, use CPU mining
	}
	err = gui.miner.WriteConfig(
		poolAddress,
		gui.config.Address,
		gui.config.CoinAlgo,
		gui.config.XmrigAlgo,
		gui.config.XmrigVariant,
		miner.ProcessingConfig{
			Threads:  newConfig.Threads,
			MaxUsage: newConfig.MaxCPU,
		})
	if err != nil {
		_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
			Data: fmt.Sprintf("Unable to configure miner."+
				"Please check that you are connected to the internet."+
				"<br/>The error was '%s'", err),
		})
		// Give the UI some time to display the message
		time.Sleep(time.Second * 15)
		gui.logger.Fatalf("Unable to configure miner: '%s'", err)
	}

	// Save the core miner config
	gui.logger.Debug("Writing GUI config")
	err = gui.SaveConfig(*gui.config)
	if err != nil {
		_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
			Data: fmt.Sprintf("Unable to configure miner."+
				"Please check that you can write to the miner's installation path."+
				"<br/>The error was '%s'", err),
		})
		// Give the UI some time to display the message
		time.Sleep(time.Second * 15)
		gui.logger.Fatalf("Unable to configure miner: '%s'", err)
	}
	gui.logger.WithFields(logrus.Fields{
		"type": minerType,
	}).Info("Miner configured")
}

// startMiner starts the miner
func (gui *GUI) startMiner() {
	err := gui.miner.Start()
	if err != nil {
		_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
			Data: fmt.Sprintf("Unable to start '%s' miner, please check that you "+
				"can run the miner from your installation directory."+
				"<br/>The error was '%s'", gui.miner.GetName(), err),
		})
		// Give the UI some time to display the message
		time.Sleep(time.Second * 15)
		gui.logger.Fatalf("Error starting '%s': %s", gui.miner.GetName(), err)
	}
	gui.logger.Infof("Started '%s' miner", gui.miner.GetName())
}

// stopMiner stops the miner
func (gui *GUI) stopMiner() error {
	if gui.miner == nil {
		return nil
	}
	err := gui.miner.Stop()
	if err != nil {
		_ = gui.sendElectronCommand("fatal_error", ElectronMessage{
			Data: fmt.Sprintf("Unable to stop miner.."+
				"Please close the GUI miner and open it again."+
				"<br/>The error was '%s'", err),
		})
		gui.logger.Errorf("Unable to stop miner '%s': %s", gui.miner.GetName(), err)
		return err
	}
	gui.logger.Infof("Stopped '%s' miner", gui.miner.GetName())
	return nil
}

// updateNetworkStats is a single stat update for network and payment info
func (gui *GUI) updateNetworkStats() {
	gui.logger.WithField(
		"hashrate", gui.lastHashrate,
	).Debug("Fetching network stats")
	// On firstrun we won't have a config yet
	if gui.config == nil {
		gui.logger.Warning("No config set yet")
		return
	}
	stats, err := gui.GetStats(gui.config.PoolID, gui.lastHashrate, gui.config.Mid)
	if err != nil {
		gui.logger.Warningf("Unable to get network stats: %s", err)
	} else {
		err := bootstrap.SendMessage(gui.window, "network_stats", stats)
		if err != nil {
			gui.logger.Errorf("Unable to send stats to front-end: %s", err)
		}
	}
}

// updateMiningStats retrieves the miner's stats and updates
// the front-end
func (gui *GUI) updateMiningStatsLoop() {
	lastGraphUpdate := time.Now()
	for _ = range gui.miningStatsTicker.C {
		if gui.miner == nil {
			// No miner set up yet.. wait more
			gui.logger.Debug("Miner not set up yet, try again later")
			continue
		}
		gui.logger.Debug("Fetching mining stats")
		stats, err := gui.miner.GetStats()
		if err != nil {
			gui.logger.Debugf("Unable to get mining stats, miner not available yet?: %s", err)
		} else {
			if gui.lastHashrate == 0 && stats.Hashrate > 0 {
				gui.lastHashrate = stats.Hashrate
				// The first time we get a hashrate, update the BLOC amount so that the
				// user doesn't think it doesn't work
				gui.updateNetworkStats()
			}
			gui.lastHashrate = stats.Hashrate
			stats.Address = gui.config.Address

			if time.Since(lastGraphUpdate).Minutes() >= 1 {
				lastGraphUpdate = time.Now()
				stats.UpdateGraph = true
			}
			statBytes, _ := json.Marshal(&stats)
			err = bootstrap.SendMessage(gui.window, "miner_stats", string(statBytes))
			if err != nil {
				gui.logger.Errorf("Unable to send miner stats to front-end: %s", err)
			}
		}
	}
}

// sendElectronCommand sends the given data to Electron under the command name
func (gui *GUI) sendElectronCommand(
	name string,
	data interface{}) error {
	dataBytes, err := json.Marshal(&data)
	if err != nil {
		return err
	}
	return bootstrap.SendMessage(gui.window, name, string(dataBytes))
}
