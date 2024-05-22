import React from "react";
import ThemeSwitcher from "./ThemeSwitcher";
import VerbisIcon from "../verbis.svg";
import { AppScreen } from "../types";
import { CogIcon, XMarkIcon } from "@heroicons/react/24/solid";

interface Props {
  navigate: (screen: AppScreen) => void;
  navigateBack: () => void;
  currentScreen: AppScreen;
}

const NavbarComponent: React.FC<Props> = ({
  navigate,
  navigateBack,
  currentScreen,
}) => {
  return (
    <div className="navbar bg-base-100">
      <div className="navbar-start"></div>
      <div className="navbar-center">
        <VerbisIcon className="h-6 w-6" />
      </div>
      <div className="navbar-end">
        <ThemeSwitcher />
        {currentScreen == AppScreen.SETTINGS ? (
          <button onClick={() => navigateBack()}>
            <XMarkIcon className="h-6 w-6" />
          </button>
        ) : (
          <button onClick={() => navigate(AppScreen.SETTINGS)}>
            <CogIcon className="h-6 w-6" />
          </button>
        )}
      </div>
    </div>
  );
};

export default NavbarComponent;