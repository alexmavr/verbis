import { useState, useEffect } from "react";
import axios from "axios";
import WelcomeComponent from "./WelcomeComponent";
import ChatComponent from "./ChatComponent";
import ConnectorsComponent from "./ConnectorsComponent";

enum Step {
  WELCOME = 0,
  GOOGLE_INIT,
  PROMPT,
}

export default function () {
  const [step, setStep] = useState<Step>(Step.WELCOME);
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
    <div className="drag">
      <div className="mx-auto flex min-h-screen w-full flex-col justify-between bg-white px-4">
        {step == Step.WELCOME && (
          <WelcomeComponent setStep={setStep} loading={loading} />
        )}
        {step === Step.GOOGLE_INIT && <ConnectorsComponent setStep={setStep} />}
        {step === Step.PROMPT && <ChatComponent />}
      </div>
    </div>
  );
}
