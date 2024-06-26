import { PaperAirplaneIcon } from "@heroicons/react/24/solid";
import React, { useEffect, useRef, useState } from "react";
import { generate } from "../client";
import GDriveLogo from "../../assets/connectors/gdrive.svg";
import GMailLogo from "../../assets/connectors/gmail.svg";
import OutlookLogo from "../../assets/connectors/outlook.svg";
import SlackLogo from "../../assets/connectors/slack.svg";
import { AppScreen, ResultSource } from "../types";
import SidebarComponent from "./SidebarComponent";
import { Conversation } from "../types";

interface Props {
  navigate: (screen: AppScreen) => void;
}

const Logos: { [key: string]: React.FC<React.SVGProps<SVGSVGElement>> } = {
  googledrive: GDriveLogo,
  gmail: GMailLogo,
  outlook: OutlookLogo,
  slack: SlackLogo,
};

const ChatComponent: React.FC<Props> = ({ navigate }) => {
  const conversationContainer = useRef<HTMLDivElement>(null);
  const [promptText, setPromptText] = useState(""); // State to store input from the textbox
  const [loading, setLoading] = useState(false); // State for the spinner
  const promptInputRef = useRef<HTMLTextAreaElement>(null);
  const [placeholder, setPlaceholder] = useState("How can I help?");
  const countRef = useRef(0); // To keep track of the ellipsis state
  const [conversationHistory, setConversationHistory] = useState([]);
  const [conversationId, setConversationId] = useState<string | null>(null); // State for conversation ID
  const [currentConversation, setCurrentConversation] =
    useState<Conversation | null>(null); // Current Conversation
  const controller = new AbortController(); // For handling cancellation

  // Function to truncate string
  const truncateString = (str: string, maxLength: number) => {
    if (str.length <= maxLength) {
      return str;
    }
    return str.substring(0, maxLength) + "...";
  };

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

  // Scroll to bottom when new prompt submitted or response received
  useEffect(() => {
    smoothScrollToBottom();
  }, [conversationHistory.length]);

  // Adjust height of textarea while prompt is being typed
  useEffect(() => {
    const textarea = promptInputRef.current;
    if (textarea) {
      textarea.style.height = "auto";
      textarea.style.height = `${textarea.scrollHeight}px`;
    }
  }, [promptText]);

  // Show animated ellipsis when loading in the prompt text area
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

  useEffect(() => {
    setConversationHistory(currentConversation?.history || []);
    setConversationId(currentConversation?.id || null);
  }, [currentConversation]);

  const triggerPrompt = async () => {
    setLoading(true); // Show loading state
    setPromptText(""); // Clear input after sending

    const previousPrompt = promptText.trim();
    if (!previousPrompt) return; // Do nothing if the prompt is empty

    const history = conversationHistory.map((item) => ({
      role: item.role,
      content: item.content,
    }));

    const assistantResponseIndex = conversationHistory.length + 1; // zero-indexed, user + assistant message from now

    try {
      if (conversationId) {
        const { sources: sources, generator } = await generate(
          previousPrompt,
          conversationId
        );
        // Create an entry for the assistant's response to accumulate content
        setConversationHistory((conv) => [
          ...conv,
          { role: "user", content: previousPrompt },
          {
            role: "assistant",
            content: "",
            sources: sources,
          },
        ]);

        let accumulatedContent = "";
        // Process each generated chunk as it arrives
        for await (const chunk of generator) {
          accumulatedContent += chunk.content;
          setConversationHistory((conv) => {
            const newConv = [...conv];
            newConv[assistantResponseIndex] = {
              ...newConv[assistantResponseIndex],
              content: accumulatedContent,
            };
            return newConv;
          });
        }
      } else {
        console.error("No conversation ID available");
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
      <SidebarComponent
        selectedConversation={currentConversation}
        setSelectedConversation={setCurrentConversation}
      />
      <div className="flex h-screen flex-col">
        <div
          ref={conversationContainer}
          className="max-h-[calc(100vh-140px)] flex-grow overflow-y-auto "
        >
          {/* Adjust paddingBottom to accommodate the prompt area */}
          {/* Conversation history */}
          {conversationHistory.length > 0 && (
            <div className="mr-4 mt-auto flex flex-col">
              {conversationHistory.map((item, index) => (
                <div key={index} className={`${item.role}`}>
                  {item.role === "user" ? (
                    // User message
                    <div className="flex justify-end">
                      <div className="card w-96 border-1 bg-base-200">
                        <div className="card-body !p-4">
                          <p>{item.content}</p>
                          <div className="card-actions justify-end">
                            {/* TODO: Feedback actions */}
                          </div>
                        </div>
                      </div>
                    </div>
                  ) : (
                    // Assistant message
                    <div className="m-4">
                      <div className="text-justify">
                        {item.content}
                        {item.hasOwnProperty("sources") &&
                          item.sources.map(
                            (source: ResultSource, sourceIndex: number) => {
                              const LogoComponent = Logos[source.type];
                              return (
                                <div
                                  key={sourceIndex}
                                  className="flex items-center"
                                >
                                  <LogoComponent className="mr-1 h-4 w-4" />
                                  <a
                                    href={source.url}
                                    target="none"
                                    className="mr-1 text-blue-600 underline visited:text-purple-600 hover:text-blue-800"
                                    onClick={(e) => {
                                      e.preventDefault();
                                      e.stopPropagation();
                                      require("electron").shell.openExternal(
                                        source.url
                                      );
                                    }}
                                  >
                                    {truncateString(source.title, 30)}
                                  </a>
                                </div>
                              );
                            }
                          )}
                      </div>
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}
        </div>

        {/* Prompt input and button */}
        <div className="fixed inset-x-0 bottom-0 left-64 flex items-center bg-white px-4 py-2">
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
            className={`flex-grow resize-none overflow-hidden rounded border border-gray-300 p-1 pr-16 ${
              loading ? "disabled:cursor-not-allowed disabled:opacity-50" : ""
            }`}
            disabled={loading}
          />
          <button
            onClick={triggerPrompt}
            className={`absolute bottom-4 right-4 mb-2 mr-2 flex h-8 w-8 items-center justify-center rounded-full bg-blue-500 font-bold text-white hover:bg-blue-700 ${
              loading ? "disabled:cursor-not-allowed disabled:opacity-50" : ""
            }`}
            disabled={loading}
          >
            {loading ? (
              <p className="loading-spinner"></p>
            ) : (
              <PaperAirplaneIcon className=" p-1 text-white" />
            )}
          </button>
        </div>
      </div>
    </>
  );
};

export default ChatComponent;
