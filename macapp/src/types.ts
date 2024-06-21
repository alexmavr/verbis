export enum AppScreen {
  WELCOME = 0,
  CHAT,
  CONNECTORS,
  SETTINGS,
}

export interface ResultSource {
  title: string;
  url: string;
  type: string;
}

export interface Conversation {
  id: string;
  created_at: string;
  updated_at: string;
  title: string;
  history?: any[];
  chunks?: any[];
  time_period?: string; // Optional initially
}
