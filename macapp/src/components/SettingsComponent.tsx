import React, { useEffect, useState } from "react";
import { AppScreen } from "../types";
import {
  connector_auth_setup,
  connector_init,
} from "../client";
import { XMarkIcon } from "@heroicons/react/24/solid";
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
import TrelloLogo from "../../assets/connectors/trello.svg";
import ZendeskLogo from "../../assets/connectors/zendesk.svg";
import ZoomLogo from "../../assets/connectors/zoom.svg";

const saasApps = [
  {
    name: "GDrive",
    logo: GDriveLogo,
    description: "Google Drive",
    available: true,
    connect: async () => {
      try {
        let conn_id = await connector_init("googledrive");
        await connector_auth_setup(conn_id);
      } catch (e) {
        console.error("could not install: ", e);
      } finally {
        getCurrentWindow().show();
        getCurrentWindow().focus();
      }
    },
  },
  {
    name: "Dropbox",
    logo: DropboxLogo,
    description: "Dropbox",
    available: false,
  },
  {
    name: "Microsoft Teams",
    logo: MSTeamsLogo,
    description: "MS Teams",
    available: false,
  },
  {
    name: "Confluence",
    logo: ConfluenceLogo,
    description: "Confluence",
    available: false,
  },
  {
    name: "Google Calendar",
    logo: GCalLogo,
    description: "Google Calendar",
    available: false,
  },
  {
    name: "Gitlab",
    logo: GitlabLogo,
    description: "Gitlab",
    available: false,
  },
  {
    name: "Gmail",
    logo: GmailLogo,
    description: "Gmail",
    available: true,
    connect: async () => {
      try {
        let conn_id = await connector_init("gmail");
        await connector_auth_setup(conn_id);
      } catch (e) {
        console.error("could not install: ", e);
      } finally {
        getCurrentWindow().show();
        getCurrentWindow().focus();
      }
    },
  },
  {
    name: "Hubspot",
    logo: HubspotLogo,
    description: "Hubspot",
    available: false,
  },
  {
    name: "Trello",
    logo: TrelloLogo,
    description: "Trello",
    available: false,
  },
  {
    name: "Zendesk",
    logo: ZendeskLogo,
    description: "Zendesk",
    available: false,
  },
  {
    name: "Zoom",
    logo: ZoomLogo,
    description: "Zoom",
    available: false,
  },
].sort((a, b) => a.name.localeCompare(b.name));

const AppCatalog: React.FC = () => {
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
                      : () => {
                          console.log("Requested", app.name);
                        }
                  }
                >
                  Connect
                </button>
              </div>
            </div>
          );
        })}
      </div>
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
