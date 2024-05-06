import React, { useEffect, useState } from "react";
import LamoidIcon from "../lamoid.svg";
import { AppScreen } from "../types";
import axios from "axios";

interface Props {
  navigate: (screen: AppScreen) => void;
}

const WelcomeComponent: React.FC<Props> = ({ navigate }) => {
  const [loading, setLoading] = useState(true); // State for the spinner

  useEffect(() => {
    const checkHealth = async () => {
      try {
        await axios.get("http://localhost:8081/health");
        setLoading(false); // Turn off spinner on successful response
      } catch (error) {
        console.error("Error checking health: ", error);
        setTimeout(checkHealth, 3000); // Retry after 3 seconds if the request fails
      }
    };

    checkHealth();
  }, []);

  return (
    <>
      <div className="mx-auto text-center">
        <h1 className="mb-6 mt-4 text-2xl tracking-tight text-gray-900">
          Welcome to Lamoid
        </h1>
        {loading ? (
          <div className="spinner">Lamoid is still starting...</div>
        ) : (
          <>
            <p className="mx-auto w-[65%] text-sm text-gray-400">
              Let's get you up and running.
            </p>
            <button
              onClick={() => navigate(AppScreen.GOOGLE_INIT)}
              className="no-drag rounded-dm mx-auto my-8 w-[40%] rounded-md bg-black px-4 py-2 text-sm text-white hover:brightness-110"
            >
              Google sync
            </button>
          </>
        )}
      </div>
      <div className="mx-auto">
        <LamoidIcon />
      </div>
    </>
  );
};

export default WelcomeComponent;
