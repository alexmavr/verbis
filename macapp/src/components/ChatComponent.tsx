import { getCurrentWindow } from "@electron/remote";
import { PaperAirplaneIcon } from "@heroicons/react/24/solid";
import React, { useEffect, useRef, useState } from "react";
import { generate } from "../client";
import { CogIcon } from "@heroicons/react/24/solid";
import SettingsComponent from "./SettingsComponent";
import { AppScreen } from "../types";

interface Props {
  navigate: (screen: AppScreen) => void;
}

const ChatComponent: React.FC<Props> = ({ navigate }) => {
  const conversationContainer = useRef<HTMLDivElement>(null);
  const [promptText, setPromptText] = useState(""); // State to store input from the textbox
  const [loading, setLoading] = useState(false); // State for the spinner
  const promptInputRef = useRef<HTMLTextAreaElement>(null);
  const [placeholder, setPlaceholder] = useState("How can I help?");
  const countRef = useRef(0); // To keep track of the ellipsis state
  const [conversation, setConversation] = useState([]);
  const controller = new AbortController(); // For handling cancellation

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

  useEffect(() => {
    return () => {
      controller.abort(); // Abort fetch on cleanup
    };
  }, []); 

  const triggerPrompt = async () => {
    setLoading(true); // Show loading state
    setPromptText(""); // Clear input after sending
  
    const previousPrompt = promptText.trim();
    if (!previousPrompt) return; // Do nothing if the prompt is empty
  
    const history = conversation.map((item) => ({
      role: item.role,
      content: item.content,
    }));
  
    const assistantResponseIndex = conversation.length + 1; // zero-indexed, user + assistant message from now

    try {
      const { initialUrls, generator } = await generate(previousPrompt, history);
      // Create an entry for the assistant's response to accumulate content
      setConversation(conv => [
        ...conv,
        { role: "user", content: previousPrompt },
        {
          role: "assistant",
          content: "",
          urls: initialUrls,
        }
      ]);

      let accumulatedContent = "";
      // Process each generated chunk as it arrives
      for await (const chunk of generator) {
        accumulatedContent += chunk.content;
        setConversation(conv => {
          const newConv = [...conv];
          newConv[assistantResponseIndex] = {
            ...newConv[assistantResponseIndex],
            content: accumulatedContent
          };
          return newConv;
        });
      }
    } catch (e) {
      console.error("Error during prompt generation: ", e);
      setPromptText(previousPrompt); // Restore the prompt text if there's an error
    } finally {
      setLoading(false);
    }
  };
  
  return (
    <>
      <div className="fixed right-4 top-4">
        <button onClick={() => navigate(AppScreen.SETTINGS)}>
          <CogIcon className="h-6 w-6" />
        </button>
      </div>
      <div className="flex h-screen flex-col justify-between">
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
              <div key={index} className={`mb-1 rounded p-1 ${item.role}`}>
                <div className="p-2">
                  {item.content}
                  <p>
                    {item.hasOwnProperty("urls") &&
                      item.urls.map((url: string, urlIndex: number) => (
                        <a
                          key={urlIndex}
                          href={url}
                          target="none"
                          className="mr-1 text-blue-600 underline visited:text-purple-600 hover:text-blue-800"
                          onClick={(e) => {
                            e.preventDefault();
                            e.stopPropagation();
                            require("electron").shell.openExternal(url);
                          }}
                        >
                          {urlIndex + 1}
                        </a>
                      ))}
                  </p>
                </div>
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
              loading ? "disabled:cursor-not-allowed disabled:opacity-50" : ""
            }`}
            disabled={loading}
          />
          <button
            onClick={triggerPrompt}
            className={`absolute bottom-4 right-4 mb-2 mr-2 flex h-10 w-10 items-center justify-center rounded-full bg-blue-500 font-bold text-white hover:bg-blue-700 ${
              loading ? "disabled:cursor-not-allowed disabled:opacity-50" : ""
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
  );
};

export default ChatComponent;
