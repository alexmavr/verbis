import { useState } from "react";
import WelcomeComponent from "./WelcomeComponent";
import ChatComponent from "./ChatComponent";
import { AppScreen } from "../types";
import SettingsComponent from "./SettingsComponent";
import NavbarComponent from "./NavbarComponent";

export default function () {
  const [currentScreen, setCurrentScreen] = useState<AppScreen>(
    AppScreen.WELCOME
  );
  const [previousScreen, setPreviousScreen] = useState<AppScreen | null>(null);

  const navigateToScreen = (screen: AppScreen) => {
    setPreviousScreen(currentScreen);
    setCurrentScreen(screen);
  };
  const navigateBack = () => {
    navigateToScreen(previousScreen ?? AppScreen.WELCOME);
  };

  return (
    <div className="drag">
      {currentScreen != AppScreen.WELCOME && (
        <NavbarComponent
          navigate={navigateToScreen}
          navigateBack={navigateBack}
          currentScreen={currentScreen}
        />
      )}
      <div className="mx-auto flex min-h-screen w-full flex-col justify-between px-4">
        {currentScreen == AppScreen.WELCOME && (
          <WelcomeComponent navigate={navigateToScreen} />
        )}
        {currentScreen === AppScreen.CHAT && (
          <ChatComponent navigate={navigateToScreen} />
        )}
        {currentScreen === AppScreen.SETTINGS && <SettingsComponent />}
      </div>
    </div>
  );
}
