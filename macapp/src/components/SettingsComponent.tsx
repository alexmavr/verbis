import React, { useEffect, useState } from "react";
import { AppScreen } from "../types";
import { list_connectors } from "../client";
import { XMarkIcon } from "@heroicons/react/24/solid";

interface Props {
  navigate: (screen: AppScreen) => void;
}

const SettingsComponent: React.FC<Props> = ({ navigate }) => {
  const [connectorList, setConnectorList] = useState([]);
  const getConnectorList = async () => {
    const response = await list_connectors();
    setConnectorList(response);
    try {
    } catch (error) {
      console.error("Failed to retrieve connectors:", error);
    }
  };

  // Run on load
  useEffect(() => {
    getConnectorList();
  }, []);

  return (
    <div>
      <div className="fixed right-4 top-4">
        <button onClick={() => navigate(AppScreen.PROMPT)}>
          <XMarkIcon className="h-6 w-6" />
        </button>
      </div>
      <div className="flex h-screen flex-col justify-between">
        <h1 className="mt-4 text-center text-2xl tracking-tight text-gray-900">
          Settings Screen
        </h1>
        {Object.values(connectorList).map((connector, index) => (
          <div key={index} className="mb-2 border-b-2">
            <h2>{connector.name}</h2>
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
