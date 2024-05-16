import React, { useEffect, useState } from "react";
import { AppScreen } from "../types";
import {
  google_auth_setup,
  google_init,
  force_sync,
  list_connectors,
} from "../client";
import { XMarkIcon } from "@heroicons/react/24/solid";
import { getCurrentWindow } from "@electron/remote";

interface Props {
  navigate: (screen: AppScreen) => void;
  navigateBack: () => void;
}

const SettingsComponent: React.FC<Props> = ({ navigate, navigateBack }) => {
  const [connectorList, setConnectorList] = useState([]);
  const getConnectorList = async () => {
    console.log("Getting connector list");
    const response = await list_connectors();
    setConnectorList(response);
    try {
    } catch (error) {
      console.error("Failed to retrieve connectors:", error);
    }
  };

  // Run on load
  useEffect(() => {
    // run once on load and then poll
    getConnectorList();
    const intervalId = setInterval(getConnectorList, 2000);

    return () => clearInterval(intervalId);
  }, []);

  return (
    <div>
      <div className="fixed right-4 top-4">
        <button onClick={() => navigateBack()}>
          <XMarkIcon className="h-6 w-6" />
        </button>
      </div>
      <div className="flex h-screen flex-col justify-between">
        <h1 className="mt-4 text-center text-2xl tracking-tight text-gray-900">
          Settings Screen
        </h1>
        <div className="mx-auto">
          <h2>Google Setup</h2>
          <button
            onClick={async () => {
              try {
                let conn_id = await google_init();
                await google_auth_setup(conn_id);
                navigate(AppScreen.PROMPT);
              } catch (e) {
                console.error("could not install: ", e);
              } finally {
                getCurrentWindow().show();
                getCurrentWindow().focus();
              }
            }}
            className="no-drag rounded-dm mx-auto rounded-md bg-black px-4 py-2 text-sm text-white hover:brightness-110"
          >
            Login with Google
          </button>
          <p className="mx-auto my-4 w-[70%] text-xs text-gray-400">
            Your browser will open to configure the OAuth credentials.
          </p>
        </div>
        <button className="sync-button" onClick={force_sync}>Force Sync</button>
        {Object.values(connectorList).map((connector, index) => (
          <div key={index} className="mb-2 border-b-2">
            <h2>{connector.type.toString()}</h2>
            <h4>{connector.user.toString()} </h4>
            <p>Auth Valid: {connector.auth_valid.toString()}</p>
            <p>Syncing: {connector.syncing.toString()}</p>
            <p>Last Sync: {connector.last_sync}</p>
            <p>Number of Documents: {connector.num_documents}</p>
            <p>Number of Chunks: {connector.num_chunks}</p>
          </div>
        ))}
      </div>
    </div>
  );
};

export default SettingsComponent;
