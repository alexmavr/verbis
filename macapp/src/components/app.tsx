import { useState, useEffect } from "react";
import axios from "axios";
import WelcomeComponent from "./WelcomeComponent";
import ChatComponent from "./ChatComponent";
import ConnectorsComponent from "./ConnectorsComponent";
import { AppScreen } from "../types";

export default function () {
  const [currentScreen, setCurrentScreen] = useState<AppScreen>(
    AppScreen.WELCOME
  );
  const [loading, setLoading] = useState(true); // State for the spinner

  const navigateToScreen = (screen: AppScreen) => {
    setCurrentScreen(screen);
  };

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
    <div className="drag">
      <div className="mx-auto flex min-h-screen w-full flex-col justify-between bg-white px-4">
        {currentScreen == AppScreen.WELCOME && (
          <WelcomeComponent navigate={navigateToScreen} loading={loading} />
        )}
        {currentScreen === AppScreen.GOOGLE_INIT && (
          <ConnectorsComponent navigate={navigateToScreen} />
        )}
        {currentScreen === AppScreen.PROMPT && (
          <ChatComponent navigate={navigateToScreen} />
        )}
      </div>
    </div>
  );
}
