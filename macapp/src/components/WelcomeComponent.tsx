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
        const response = await axios.get("http://localhost:8081/health");
        const data = response.data;

        if (data.boot_state === "generating") {
          setLoading(false); // Turn off spinner on successful response
          navigate(AppScreen.CHAT); // Redirect to the prompt screen
        } else {
          setTimeout(checkHealth, 3000); // Retry after 3 seconds if the request fails
        }
      } catch (error) {
        console.error("Error checking health: ", error);
        setTimeout(checkHealth, 3000); // Retry after 3 seconds if the request fails
      }
    };

    checkHealth();
  }, []);

  return (
    <>
      <div className="flex h-screen flex-col items-center justify-center text-center">
        <h1 className="m-4 text-2xl tracking-tight">Welcome to Verbis AI</h1>
        <div className={`mt-4 ${loading ? "verbis-loading" : ""}`}>
          <VerbisIcon className="h-24 w-24" />
        </div>
        {loading ? (
          <p className="mx-auto mt-8 w-[65%]">Setting things up...</p>
        ) : (
          <>
            {/* TODO: Unreachable code if we auto redirect to Prompt screen on load. Clean up */}
            <p className="mx-auto mt-8 w-[65%] text-sm text-gray-400">
              We're ready for you!
            </p>
            <button
              onClick={() => navigate(AppScreen.CHAT)}
              className="no-drag rounded-dm mx-auto my-8 rounded-md bg-black px-4 py-2 text-sm text-white hover:brightness-110"
            >
              Continue
            </button>
          </>
        )}
      </div>
    </>
  );
};

export default WelcomeComponent;
