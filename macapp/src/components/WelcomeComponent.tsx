import React, { useEffect, useState } from "react";
import VerbisIcon from "../verbis.svg";
import { AppScreen } from "../types";
import axios from "axios";
import { CogIcon } from "@heroicons/react/24/solid";

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
        navigate(AppScreen.PROMPT); // Redirect to the prompt screen
      } catch (error) {
        console.error("Error checking health: ", error);
        setTimeout(checkHealth, 3000); // Retry after 3 seconds if the request fails
      }
    };

    checkHealth();
  }, []);

  return (
    <>
      <div className="fixed right-4 top-4">
        <button onClick={() => navigate(AppScreen.SETTINGS)}>
          <CogIcon className="h-6 w-6" />
        </button>
      </div>
      <div className="mx-auto text-center">
        <h1 className="mb-6 mt-4 text-2xl tracking-tight text-gray-900">
          Welcome to verbis
        </h1>
        {loading ? (
          <div className="spinner">verbis is still starting...</div>
        ) : (
          <>
            <p className="mx-auto w-[65%] text-sm text-gray-400">
              Let's get you up and running.
            </p>
            <button
              onClick={() => navigate(AppScreen.PROMPT)}
              className="no-drag rounded-dm mx-auto my-8 rounded-md bg-black px-4 py-2 text-sm text-white hover:brightness-110"
            >
              Continue
            </button>
          </>
        )}
      </div>
      <div className="mx-auto">
        <VerbisIcon />
      </div>
    </>
  );
};

export default WelcomeComponent;
