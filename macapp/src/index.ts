import { spawn, ChildProcess } from 'child_process'
import { app, dialog, Tray, Menu, BrowserWindow, MenuItemConstructorOptions, nativeTheme } from 'electron'
import Store from 'electron-store'
import winston from 'winston'
import 'winston-daily-rotate-file'
import * as path from 'path'

require('@electron/remote/main').initialize()

const isDevelopment = process.env.NODE_ENV === 'development'

const store = new Store()

let welcomeWindow: BrowserWindow | null = null

declare const MAIN_WINDOW_WEBPACK_ENTRY: string

const logger = winston.createLogger({
  transports: [
    new winston.transports.Console(),
    new winston.transports.File({
      filename: path.join(app.getPath('home'), '.verbis', 'logs', 'server.log'),
      maxsize: 1024 * 1024 * 20,
      maxFiles: 5,
    }),
  ],
  format: winston.format.printf(info => info.message),
})

app.on("ready", () => {
  const gotTheLock = app.requestSingleInstanceLock();
  if (!gotTheLock) {
    app.exit(0);
    return;
  }

  app.on("second-instance", () => {
    if (app.hasSingleInstanceLock()) {
      app.releaseSingleInstanceLock();
    }

    if (proc) {
      proc.off("exit", restart);
      proc.kill();
    }

    app.exit(0);
  });

  app.focus({ steal: false });

  init();
});

function firstRunWindow() {
  // Create the browser window.
  welcomeWindow = new BrowserWindow({
    width: 1000,
    height: 800,
    frame: true,
    fullscreenable: false,
    resizable: true,
    movable: true,
    show: false,
    webPreferences: {
      nodeIntegration: true,
      contextIsolation: false,
    },
  });

  require("@electron/remote/main").enable(welcomeWindow.webContents);

  welcomeWindow.loadURL(MAIN_WINDOW_WEBPACK_ENTRY);
  welcomeWindow.on("ready-to-show", () => welcomeWindow.show());
  welcomeWindow.on("closed", () => {
    if (process.platform === "darwin") {
      app.dock.hide();
    }
  });

  welcomeWindow.webContents.on("before-input-event", (event, input) => {
    if (input.key === "F12" && input.type === "keyDown") {
      if (welcomeWindow.webContents.isDevToolsOpened()) {
        welcomeWindow.webContents.closeDevTools();
      } else {
        welcomeWindow.webContents.openDevTools();
      }
    }
  });
}

let tray: Tray | null = null;
let updateAvailable = false;
const assetPath = app.isPackaged
  ? process.resourcesPath
  : path.join(__dirname, "..", "..", "assets");

function trayIconPath() {
  return nativeTheme.shouldUseDarkColors
    ? updateAvailable
      ? path.join(assetPath, "iconDarkUpdateTemplate.png")
      : path.join(assetPath, "iconDarkTemplate.png")
    : updateAvailable
    ? path.join(assetPath, "iconUpdateTemplate.png")
    : path.join(assetPath, "iconTemplate.png");
}

function updateTray() {
  // For some reason it hangs on first run
  const menu = Menu.buildFromTemplate([
    { role: "quit", label: "Quit verbis", accelerator: "Command+Q" },
  ]);

  if (!tray) {
    tray = new Tray(trayIconPath());
  }

  tray.setContextMenu(menu);
  tray.setImage(trayIconPath());
}

let proc: ChildProcess = null;

function server() {
  const binary = app.isPackaged
    ? path.join(process.resourcesPath, "verbis")
    : path.resolve(process.cwd(), "..", "verbis");

  proc = spawn(binary, []);

  proc.stdout.on("data", (data) => {
    logger.info(data.toString().trim());
  });

  proc.stderr.on("data", (data) => {
    logger.error(data.toString().trim());
  });

  proc.on("exit", (code) => {
    logger.error(`Server process exited with code ${code}`);
    restart();
  });
}

function restart() {
  setTimeout(server, 1000);
}

app.on("before-quit", () => {
  if (proc) {
    proc.off("exit", restart);
    proc.kill("SIGINT"); // send SIGINT signal to the server, which also stops any loaded llms
  }
});

function init() {
  logger.info("Starting Verbis");
  //updateTray()

  if (process.platform === "darwin" && !isDevelopment) {
    if (app.isPackaged) {
      logger.info("In packaged");
      if (!app.isInApplicationsFolder()) {
        const chosen = dialog.showMessageBoxSync({
          type: "question",
          buttons: ["Move to Applications", "Do Not Move"],
          message:
            "Verbis works best when run from the Applications directory.",
          defaultId: 0,
          cancelId: 1,
        });

        if (chosen === 0) {
          try {
            app.moveToApplicationsFolder({
              conflictHandler: (conflictType) => {
                if (conflictType === "existsAndRunning") {
                  dialog.showMessageBoxSync({
                    type: "info",
                    message: "Cannot move to Applications directory",
                    detail:
                      "Another version of Verbis is currently running from your Applications directory. Close it first and try again.",
                  });
                }
                return true;
              },
            });
            return;
          } catch (e) {
            logger.error(
              `[Move to Applications] Failed to move to applications folder - ${e.message}}`
            );
          }
        }
      }
    }
  }

  if (!isDevelopment) {
    server();
  }

  logger.info("Running first window");
  firstRunWindow();
  logger.info("First window ran");
}