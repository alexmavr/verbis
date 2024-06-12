import React, { useEffect, useState } from "react";
import { force_sync, list_connectors, connector_delete } from "../client";
import GDriveLogo from "../../assets/connectors/gdrive.svg";
import GMailLogo from "../../assets/connectors/gmail.svg";
import OutlookLogo from "../../assets/connectors/outlook.svg";
import { ArrowPathIcon, TrashIcon } from "@heroicons/react/24/solid";
import { formatDistanceToNow, differenceInYears } from "date-fns";
import ConfirmationModal from "./ConfirmationModal";

const appLogos: { [key: string]: React.FC<React.SVGProps<SVGSVGElement>> } = {
  googledrive: GDriveLogo,
  gmail: GMailLogo,
  outlook: OutlookLogo,
  // Add more mappings for other connector types
};

const ActiveConnectorsList: React.FC = () => {
  const [connectorList, setConnectorList] = useState([]);
  const [isConfirmationModalOpen, setIsConfirmationModalOpen] = useState(false);
  const [selectedConnector, setSelectedConnector] = useState(null);

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

  // Function to format last_sync date
  function renderLastSyncDate(lastSyncDate: string | number | Date): string {
    const date = new Date(lastSyncDate);

    // Check if the date is more than 1 year ago
    if (differenceInYears(new Date(), date) > 1) {
      return "-";
    } else {
      return formatDistanceToNow(date, { addSuffix: true });
    }
  }

  const openConfirmationModal = (connector_id: string) => {
    setSelectedConnector(connector_id);
    setIsConfirmationModalOpen(true);
  };
  const closeConfirmationModal = () => {
    setSelectedConnector(null);
    setIsConfirmationModalOpen(false);
  };
  const confirmDelete = async () => {
    await connector_delete(selectedConnector);
    getConnectorList();
    closeConfirmationModal();
  };

  return (
    <div>
      <div className="overflow-x-auto">
        <table className="table">
          <thead>
            <tr>
              {/* <th>
                <label>
                  <input type="checkbox" className="checkbox" />
                </label>
              </th> */}
              <th></th>
              <th></th>
              <th>Account</th>
              <th># Documents</th>
              <th># Errors</th>
              <th>Last Sync</th>
              <th></th>
            </tr>
          </thead>
          <tbody>
            {Object.values(connectorList).map((connector, index) => {
              const LogoComponent = appLogos[connector.connector_type];
              return (
                <tr key={index}>
                  {/* <th>
                  <label>
                    <input type="checkbox" className="checkbox" />
                  </label>
                </th> */}
                  <td>
                    <button className="rounded-full" onClick={force_sync}>
                      <ArrowPathIcon
                        className={`h-5 w-5 ${
                          connector.syncing ? "animate-spin" : ""
                        }`}
                        title={connector.syncing ? "Syncing..." : "Force Sync"}
                      />
                    </button>
                  </td>
                  <td>
                    {LogoComponent ? <LogoComponent className="h-5 w-5" /> : ""}
                  </td>
                  <td>{connector.user.toString()}</td>
                  <td>{connector.num_documents}</td>
                  <td>{connector.num_errors}</td>
                  <td>{renderLastSyncDate(connector.last_sync)}</td>
                  <td>
                    <TrashIcon
                      className="h-5 w-5"
                      onClick={() =>
                        openConfirmationModal(connector.connector_id)
                      }
                    />
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
      <ConfirmationModal
        isOpen={isConfirmationModalOpen}
        onClose={closeConfirmationModal}
        onConfirm={confirmDelete}
        content={"Are you sure you want to delete this connector?"}
      />
    </div>
  );
};

export default ActiveConnectorsList;
