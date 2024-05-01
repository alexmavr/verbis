import { useState, useEffect, useRef } from "react";
import Store from "electron-store";
import { getCurrentWindow, app } from "@electron/remote";
import axios from "axios";
import { PaperAirplaneIcon } from "@heroicons/react/24/solid";

import {
  google_init,
  google_sync,
  generate,
  google_auth_setup,
} from "./client";
import LamoidIcon from "./lamoid.svg";

const store = new Store();

enum Step {
  WELCOME = 0,
  GOOGLE_INIT,
  PROMPT,
}

export default function () {
  const [step, setStep] = useState<Step>(Step.WELCOME);
  const [promptText, setPromptText] = useState(""); // State to store input from the textbox
  const [loading, setLoading] = useState(true); // State for the spinner
  const [conversation, setConversation] = useState([]);
  const conversationContainer = useRef<HTMLDivElement>(null);
  const promptInputRef = useRef<HTMLTextAreaElement>(null);
  const [placeholder, setPlaceholder] = useState("How can I help?");
  const countRef = useRef(0); // To keep track of the ellipsis state

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

  const smoothScrollToBottom = () => {
    const element = conversationContainer.current;
    if (element) {
      const from = element.scrollTop;
      const to = element.scrollHeight - element.clientHeight;

      if (from === to) return; // Already at bottom

      const duration = 500; // Adjust duration as needed
      const startTime = performance.now();

      const animateScroll = (currentTime: number) => {
        const elapsedTime = currentTime - startTime;
        const fraction = Math.min(elapsedTime / duration, 1); // Ensure it doesn't go beyond 1

        const easeInOutQuad = (t: number) =>
          t < 0.5 ? 2 * t * t : -1 + (4 - 2 * t) * t;
        const newScrollTop = from + (to - from) * easeInOutQuad(fraction);

        element.scrollTop = newScrollTop;

        if (fraction < 1) {
          requestAnimationFrame(animateScroll);
        }
      };

      requestAnimationFrame(animateScroll);
    }
  };

  useEffect(() => {
    smoothScrollToBottom();
  }, [conversation.length]);

  useEffect(() => {
    const textarea = promptInputRef.current;
    if (textarea) {
      textarea.style.height = "auto";
      textarea.style.height = `${textarea.scrollHeight}px`;
    }
  }, [promptText]);

  useEffect(() => {
    if (loading) {
      setPlaceholder("Processing");
      const interval = setInterval(() => {
        const dots = countRef.current % 4;
        setPlaceholder(`Processing${".".repeat(dots)}`);
        countRef.current++;
      }, 500);

      return () => clearInterval(interval);
    } else {
      setPlaceholder("How can I help?");
      countRef.current = 0;
    }
  }, [loading]);

  // Function to handle the prompting action
  const triggerPrompt = async () => {
    // Set loading to disable input and button
    setLoading(true);
    // Remember previous prompt
    const previousPrompt = promptText;
    setPromptText("");

    if (!promptText.trim()) return; // Do nothing if the prompt is empty

    // Transform conversation history to the expected format
    const history = conversation.map((item) => ({
      role: item.role,
      content: item.content,
    }));

    // Display the submitted prompt immediately
    setConversation((conv) => [
      ...conv,
      {
        role: "user",
        content: previousPrompt,
      },
    ]);

    try {
      const { content, sourceURLs } = await generate(promptText, history);
      console.log(sourceURLs);
      // Assuming that response is just the assistant's text, adjust if it's structured differently
      setConversation((conv) => [
        ...conv,
        // { role: "user", content: promptText },
        { role: "assistant", content: content },
      ]);
      setPromptText(""); // Clear the input field after sending the prompt
    } catch (e) {
      console.error("Error during prompt generation: ", e);
      // Put the prompt back
      setPromptText(previousPrompt);
    } finally {
      setLoading(false);
      getCurrentWindow().show();
      getCurrentWindow().focus();
    }
  };

  const renderConversation = () => {
    return conversation.map((item, index) => (
      <div key={index} className={`mb-1 rounded p-1 ${item.role}`}>
        <div className="p-2">{item.content}</div>
      </div>
    ));
  };

  return (
    <div className="drag">
      <div className="mx-auto flex min-h-screen w-full flex-col justify-between bg-white px-4">
        {step === Step.WELCOME && (
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
                    onClick={() => setStep(Step.GOOGLE_INIT)}
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
        )}
        {step === Step.GOOGLE_INIT && (
          <>
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
                      setStep(Step.PROMPT);
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
        )}
        {step === Step.PROMPT && (
          <>
            <div className="mx-auto flex h-screen flex-col justify-between">
              <h1 className="mt-4 text-center text-2xl tracking-tight text-gray-900">
                Prompt to your heart's desire
              </h1>

              {/* Conversation history */}
              {conversation.length > 0 && (
                <div
                  ref={conversationContainer}
                  className="mt-5 overflow-auto overflow-y-auto pb-20 pr-2"
                >
                  {conversation.map((item, index) => (
                    <div
                      key={index}
                      className={`mb-1 rounded p-1 ${item.role}`}
                    >
                      <div className="p-2">{item.content}</div>
                    </div>
                  ))}
                </div>
              )}

              {/* Prompt input and button */}
              <div className="fixed inset-x-0 bottom-0 flex items-center p-4 shadow-lg">
                <textarea
                  ref={promptInputRef}
                  value={promptText}
                  onChange={(e) => setPromptText(e.target.value)}
                  placeholder={placeholder}
                  onKeyDown={(e) => {
                    if (e.key === "Enter" && !e.shiftKey) {
                      e.preventDefault();
                      triggerPrompt();
                    } else if (e.key === "Enter" && e.shiftKey) {
                      // setPromptText(promptText + "\n");
                    }
                  }}
                  className={`flex-grow resize-none overflow-hidden rounded border border-gray-300 p-2 pr-16 ${
                    loading
                      ? "disabled:cursor-not-allowed disabled:opacity-50"
                      : ""
                  }`}
                  disabled={loading}
                />
                <button
                  onClick={triggerPrompt}
                  className={`absolute bottom-4 right-4 mb-2 mr-2 flex h-10 w-10 items-center justify-center rounded-full bg-blue-500 font-bold text-white hover:bg-blue-700 ${
                    loading
                      ? "disabled:cursor-not-allowed disabled:opacity-50"
                      : ""
                  }`}
                  disabled={loading}
                >
                  {loading ? (
                    <p className="loading-spinner"></p>
                  ) : (
                    <PaperAirplaneIcon className=" p-2 text-white" />
                  )}
                </button>
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
