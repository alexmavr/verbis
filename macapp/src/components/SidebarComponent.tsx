import React, { useEffect, useState } from "react";
import { list_conversations } from "../client";
import { isToday, isThisWeek, isThisMonth, parseISO, format } from "date-fns";

interface Conversation {
  created_at: string;
  updated_at: string;
  title: string;
  history: any[];
  chunks: any[];
  time_period?: string; // Optional initially
}

const addTimePeriod = (conversations: Conversation[]): Conversation[] => {
  return conversations.map((conversation) => {
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
  });
};

const formatDatetime = (dateString: string) => {
  const date = parseISO(dateString);
  return format(date, "do MMMM, yyyy HH:mm");
};

const SidebarComponent: React.FC = () => {
  const [conversations, setConversations] = useState<Conversation[]>([]);
  const [todaysConvos, setTodaysConvos] = useState<Conversation[]>([]);
  const [weeksConvos, setWeeksConvos] = useState<Conversation[]>([]);
  const [monthsConvos, setMonthsConvos] = useState<Conversation[]>([]);

  useEffect(() => {
    const fetchConversations = async () => {
      const conversation_list = await list_conversations();
      const updatedConversations = addTimePeriod(conversation_list);
      setConversations(updatedConversations);
    };

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
  }, [conversations]);

  const renderConversations = (conversations: Conversation[]) => {
    return conversations.map((conversation, idx) => (
      <li key={idx} className="flex justify-between py-1">
        <div>
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
    <div className="drawer fixed mt-10">
      <input
        id="my-drawer"
        type="checkbox"
        defaultChecked
        className="drawer-toggle"
      />
      <div className="drawer-content">
        <label htmlFor="my-drawer" className="btn btn-primary drawer-button">
          Open drawer
        </label>
      </div>
      <div className="drawer-side">
        <div className="min-h-full w-64 bg-base-200 p-4 text-base-content">
          <ul className="menu">
            <li className="menu-title text-xs">
              <span>Today</span>
            </li>
            {renderConversations(todaysConvos)}
            <li className="menu-title text-xs">
              <span>Previous 7 Days</span>
            </li>
            {renderConversations(weeksConvos)}
            <li className="menu-title text-xs">
              <span>Previous 30 Days</span>
            </li>
            {renderConversations(monthsConvos)}
          </ul>
        </div>
      </div>
    </div>
  );
};

export default SidebarComponent;
