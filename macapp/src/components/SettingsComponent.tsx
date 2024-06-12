import React, { useState } from "react";
import { AppScreen } from "../types";
import {
  connector_auth_setup,
  connector_init,
  connector_request,
} from "../client";
import { getCurrentWindow } from "@electron/remote";
import ActiveConnectorsList from "./ActiveConnectorsList";
import GDriveLogo from "../../assets/connectors/gdrive.svg";
import DropboxLogo from "../../assets/connectors/dropbox.svg";
import MSTeamsLogo from "../../assets/connectors/ms_teams.svg";
import ConfluenceLogo from "../../assets/connectors/confluence.svg";
import GCalLogo from "../../assets/connectors/gcal.svg";
import GitlabLogo from "../../assets/connectors/gitlab.svg";
import GmailLogo from "../../assets/connectors/gmail.svg";
import HubspotLogo from "../../assets/connectors/hubspot.svg";
import SlackLogo from "../../assets/connectors/slack.svg";
import TrelloLogo from "../../assets/connectors/trello.svg";
import ZendeskLogo from "../../assets/connectors/zendesk.svg";
import ZoomLogo from "../../assets/connectors/zoom.svg";
import OutlookLogo from "../../assets/connectors/outlook.svg";

interface SaasApp {
  name: string;
  logo: React.ComponentType<React.SVGProps<SVGSVGElement>>;
  description: string;
  available: boolean;
  internal_name: string;
  connect?: () => Promise<void>;
  connector_request?: (showNotification: () => void) => Promise<void>;
}

const handleConnect = async (internal_name: string) => {
  try {
    let conn_id = await connector_init(internal_name);
    await connector_auth_setup(conn_id);
  } catch (e) {
    console.error(`Could not install ${internal_name}: `, e);
  } finally {
    getCurrentWindow().show();
    getCurrentWindow().focus();
  }
};

const handleConnectorRequest = async (internal_name: string, showNotification: () => void) => {
  try {
    await connector_request(internal_name);
  } catch (e) {
    console.error(`Could not request connector ${internal_name}: `, e);
  } finally {
    showNotification();
  }
};

const saasApps: SaasApp[] = [
  {
    name: "GDrive",
    logo: GDriveLogo,
    description: "Google Drive",
    available: true,
    internal_name: "googledrive",
  },
  {
    name: "Gmail",
    logo: GmailLogo,
    description: "Gmail",
    available: true,
    internal_name: "gmail",
  },
  {
    name: "Outlook",
    logo: OutlookLogo,
    description: "Outlook",
    available: true,
    internal_name: "outlook",
  },
  {
    name: "Slack",
    logo: SlackLogo,
    description: "Slack",
    available: true,
    internal_name: "slack",
  },
  {
    name: "Dropbox",
    logo: DropboxLogo,
    description: "Dropbox",
    available: false,
    internal_name: "dropbox",
  },
  {
    name: "Microsoft Teams",
    logo: MSTeamsLogo,
    description: "MS Teams",
    internal_name: "msteams",
    available: false,
  },
  {
    name: "Confluence",
    logo: ConfluenceLogo,
    description: "Confluence",
    internal_name: "confluence",
    available: false,
  },
  {
    name: "Google Calendar",
    logo: GCalLogo,
    description: "Google Calendar",
    internal_name: "googlecalendar",
    available: false,
  },
  {
    name: "Gitlab",
    logo: GitlabLogo,
    description: "Gitlab",
    internal_name: "gitlab",
    available: false,
  },

  {
    name: "Hubspot",
    logo: HubspotLogo,
    description: "Hubspot",
    internal_name: "hubspot",
    available: false,
  },
  {
    name: "Trello",
    logo: TrelloLogo,
    description: "Trello",
    internal_name: "trello",
    available: false,
  },
  {
    name: "Zendesk",
    logo: ZendeskLogo,
    description: "Zendesk",
    internal_name: "zendesk",
    available: false,
  },
  {
    name: "Zoom",
    logo: ZoomLogo,
    description: "Zoom",
    internal_name: "zoom",
    available: false,
  },
]
  .map((app) => ({
    ...app,
    connect: app.available ? () => handleConnect(app.internal_name) : undefined,
    connector_request: !app.available
      ? (showNotification: () => void) =>
          handleConnectorRequest(app.internal_name, showNotification)
      : undefined,
  }))
  .sort((a, b) => {
    if (a.available === b.available) {
      return a.name.localeCompare(b.name);
    }
    return b.available ? 1 : -1;
  });

const NotificationModal: React.FC<{ show: boolean; onClose: () => void }> = ({ show, onClose }) => {
  if (!show) return null;

  return (
    <div className="fixed inset-0 flex items-center justify-center z-50">
      <div className="fixed inset-0 bg-black opacity-50"></div>
      <div className="bg-white p-4 rounded shadow-lg z-50">
        <h2 className="text-lg font-semibold">Connector Requested</h2>
        <p>The connector has been requested for future support.</p>
        <button className="btn-primary btn mt-4" onClick={onClose}>
          Close
        </button>
      </div>
    </div>
  );
};

const AppCatalog: React.FC = () => {
  const [notificationVisible, setNotificationVisible] = useState(false);

  const showNotification = () => {
    setNotificationVisible(true);
  };

  const hideNotification = () => {
    setNotificationVisible(false);
  };

  return (
    <div className="container mx-auto p-4">
      <div className="grid grid-cols-2 gap-6 sm:grid-cols-2 md:grid-cols-3 lg:grid-cols-4">
        {saasApps.map((app, index) => {
          const LogoComponent = app.logo; // Access the component
          return (
            <div key={index} className="card shadow-lg">
              <div className="card-body">
                <div className="flex items-center space-x-4">
                  <div className="avatar">
                    <div className="h-16 w-16">
                      <LogoComponent className="h-full w-full" />
                    </div>
                  </div>
                  <div>
                    <h2 className="card-title">{app.name}</h2>
                    <p>{app.description}</p>
                  </div>
                </div>
              </div>
              <div className="card-actions justify-end p-4">
                <button
                  className="btn-primary btn"
                  onClick={
                    app.available
                      ? app.connect
                      : () => app.connector_request?.(showNotification)
                  }
                >
                  {app.available ? "Connect" : "Request"}
                </button>
              </div>
            </div>
          );
        })}
      </div>
      <NotificationModal show={notificationVisible} onClose={hideNotification} />
    </div>
  );
};

const SettingsComponent: React.FC = () => {
  return (
    <div className=" mt-14 flex h-screen flex-col justify-between">
      <h1 className="mt-4 text-center text-xl tracking-tight text-gray-900">
        Connected Apps
      </h1>
      <ActiveConnectorsList />
      <h1 className="mt-4 text-center text-xl tracking-tight text-gray-900">
        App Catalog
      </h1>

      <AppCatalog />
    </div>
  );
};

export default SettingsComponent;
