import React, { useEffect, useState } from "react";
import { force_sync, list_connectors } from "../client";
import GDriveLogo from "../../assets/connectors/gdrive.svg";
import GMailLogo from "../../assets/connectors/gmail.svg";
import {
  ArrowPathIcon,
  CheckCircleIcon,
  ExclamationCircleIcon,
} from "@heroicons/react/24/solid";
import { formatDistanceToNow, differenceInYears } from "date-fns";

const appLogos: { [key: string]: React.FC<React.SVGProps<SVGSVGElement>> } = {
  googledrive: GDriveLogo,
  gmail: GMailLogo,
  // Add more mappings for other connector types
};

const ActiveConnectorsList: React.FC = () => {
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
                </tr>
              );
            })}
          </tbody>
          {/* <tfoot>
            <tr>
              <th></th>
              <th>Connector Type</th>
              <th>User</th>
              <th>Auth Valid</th>
              <th>Syncing</th>
              <th>Last Sync</th>
              <th>Number of Documents</th>
              <th>Number of Chunks</th>
            </tr>
          </tfoot> */}
        </table>
      </div>
    </div>
  );
};

export default ActiveConnectorsList;
