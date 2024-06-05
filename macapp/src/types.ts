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
