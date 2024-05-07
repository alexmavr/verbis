import React from "react";
import { google_auth_setup, google_init } from "../client";
import { getCurrentWindow } from "@electron/remote";
import { AppScreen } from "../types";
import { CogIcon } from "@heroicons/react/24/solid";

interface Props {
  // Add your component's props here
  navigate: (screen: AppScreen) => void;
}

const ConnectorsComponent: React.FC<Props> = ({ navigate }) => {
  // Add your component's state and logic here

  return (
    <>
      <div className="fixed right-4 top-4">
        <button onClick={() => navigate(AppScreen.SETTINGS)}>
          <CogIcon className="h-6 w-6" />
        </button>
      </div>
      <div className="mx-auto flex flex-col space-y-28 text-center">
        <h1 className="mt-4 text-2xl tracking-tight text-gray-900">
          Set up your google connector
        </h1>
        <div className="mx-auto">
          <button
            onClick={async () => {
              try {
                await google_init();
                await google_auth_setup();
                navigate(AppScreen.PROMPT);
              } catch (e) {
                console.error("could not install: ", e);
              } finally {
                getCurrentWindow().show();
                getCurrentWindow().focus();
              }
            }}
            className="no-drag rounded-dm mx-auto w-[60%] rounded-md bg-black px-4 py-2 text-sm text-white hover:brightness-110"
          >
            Configure google OAuth
          </button>
          <p className="mx-auto my-4 w-[70%] text-xs text-gray-400">
            Your browser will open to configure the OAuth credentials.
          </p>
        </div>
      </div>
    </>
  );
};

export default ConnectorsComponent;
