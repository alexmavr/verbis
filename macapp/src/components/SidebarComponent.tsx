import React, { useEffect, useState } from "react";
import {
  create_conversation,
  get_conversation,
  list_conversations,
} from "../client";
import { isToday, isThisWeek, isThisMonth, parseISO, format } from "date-fns";
import {
  MagnifyingGlassIcon,
  PencilSquareIcon,
} from "@heroicons/react/24/solid";
import { Conversation } from "../types";

const addTimePeriod = (conversations: Conversation[]): Conversation[] => {
  return conversations
    .map((conversation) => {
      const createdAt = parseISO(conversation.created_at);

      let timePeriod = "";
      if (isToday(createdAt)) {
        timePeriod = "today";
      } else if (isThisWeek(createdAt, { weekStartsOn: 1 })) {
        timePeriod = "week";
      } else if (isThisMonth(createdAt)) {
        timePeriod = "month";
      }

      return { ...conversation, time_period: timePeriod };
    })
    .sort((a, b) => b.created_at.localeCompare(a.created_at));
};

const formatDatetime = (dateString: string) => {
  const date = parseISO(dateString);
  return format(date, "do MMMM, yyyy HH:mm");
};

interface Props {
  setSelectedConversation: (conversation: Conversation) => void;
  selectedConversation: Conversation | null;
}

const SidebarComponent: React.FC<Props> = ({
  selectedConversation,
  setSelectedConversation,
}) => {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [todaysConvos, setTodaysConvos] = useState<Conversation[]>([]);
  const [weeksConvos, setWeeksConvos] = useState<Conversation[]>([]);
  const [monthsConvos, setMonthsConvos] = useState<Conversation[]>([]);

  const fetchConversations = async () => {
    let conversationList = await list_conversations();
    const updatedConversations = addTimePeriod(conversationList);
    setConversations(updatedConversations);
    // If conversations exist, set selected to most recent
    if (updatedConversations.length > 0) {
      setSelectedConversation(updatedConversations[0]);
    } else {
      // If no conversations exist, create a new one
      startNewConversation();
    }
  };

  const startNewConversation = async () => {
    try {
      const newConversationId = await create_conversation();
      const newConversation = await get_conversation(newConversationId);
      fetchConversations();
      // Override fetchConversations' logic of setting current to most recent
      // by explicitly setting it to the newly created conversation
      setSelectedConversation(newConversation);
    } catch (error) {
      console.error("Failed to create conversation:", error);
    }
  };

  // fetch conversations on load
  useEffect(() => {
    fetchConversations();
  }, []);

  useEffect(() => {
    setTodaysConvos(
      conversations.filter(
        (conversation) => conversation.time_period === "today"
      )
    );
    setWeeksConvos(
      conversations.filter(
        (conversation) => conversation.time_period === "week"
      )
    );
    setMonthsConvos(
      conversations.filter(
        (conversation) => conversation.time_period === "month"
      )
    );
  }, [conversations, selectedConversation]);

  const renderConversations = (conversations: Conversation[]) => {
    return conversations.map((conversation, idx) => (
      <li
        key={idx}
        className="flex justify-between py-1"
        onClick={() => setSelectedConversation(conversation)}
      >
        <div
          className={
            selectedConversation && selectedConversation.id === conversation.id
              ? "bg-gray-200"
              : ""
          }
        >
          <h3 className="text-sm font-medium">
            {conversation.title
              ? conversation.title
              : formatDatetime(conversation.created_at)}
          </h3>
        </div>
      </li>
    ));
  };

  return (
    <div className="drawer fixed">
      <input
        id="my-drawer"
        type="checkbox"
        defaultChecked
        className="drawer-toggle"
      />
      <div className="drawer-side mt-16 w-64">
        <div className="min-h-full bg-base-200 p-4 text-base-content">
          <button className="btn btn-ghost" onClick={startNewConversation}>
            <PencilSquareIcon className="h-6 w-6" />
          </button>
          <ul className="menu overflow-y-scroll">
            {/* TODO: Uncomment and wire up for title search */}
            {/* <li className="mb-4">
              <label className="input input-bordered flex items-center gap-1 rounded bg-transparent p-2 shadow-sm">
                <input
                  type="text"
                  className="grow outline-none"
                  placeholder="Search"
                />
                <MagnifyingGlassIcon className="h-4 w-4 opacity-70" />
              </label>
            </li> */}
            <li className="menu-title text-xs">
              <span>Today</span>
            </li>
            {renderConversations(todaysConvos)}
            {weeksConvos.length > 0 && (
              <li className="menu-title text-xs">
                <span>Previous 7 Days</span>
              </li>
            )}
            {renderConversations(weeksConvos)}
            {monthsConvos.length > 0 && (
              <li className="menu-title text-xs">
                <span>Previous 30 Days</span>
              </li>
            )}
            {renderConversations(monthsConvos)}
          </ul>
        </div>
      </div>
    </div>
  );
};

export default SidebarComponent;
