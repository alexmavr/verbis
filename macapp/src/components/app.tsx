import { useState } from "react";
import WelcomeComponent from "./WelcomeComponent";
import ChatComponent from "./ChatComponent";
import ConnectorsComponent from "./ConnectorsComponent";
import { AppScreen } from "../types";
import SettingsComponent from "./SettingsComponent";

export default function () {
  const [currentScreen, setCurrentScreen] = useState<AppScreen>(
    AppScreen.WELCOME
  );

  const navigateToScreen = (screen: AppScreen) => {
    setCurrentScreen(screen);
  };

  return (
    <div className="drag">
      <div className="mx-auto flex min-h-screen w-full flex-col justify-between bg-white px-4">
        {currentScreen == AppScreen.WELCOME && (
          <WelcomeComponent navigate={navigateToScreen} />
        )}
        {currentScreen === AppScreen.GOOGLE_INIT && (
          <ConnectorsComponent navigate={navigateToScreen} />
        )}
        {currentScreen === AppScreen.PROMPT && (
          <ChatComponent navigate={navigateToScreen} />
        )}
        {currentScreen === AppScreen.SETTINGS && (
          <SettingsComponent navigate={navigateToScreen} />
        )}
      </div>
    </div>
  );
}