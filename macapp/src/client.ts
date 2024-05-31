import * as fs from 'fs'
import * as path from 'path'
import { promisify } from 'util'
import axios from 'axios';
import { json } from 'stream/consumers';
import { ResultSource } from "./types";

const app =
  process && process.type === "renderer"
    ? require("@electron/remote").app
    : require("electron").app;
const verbis = app.isPackaged
  ? path.join(process.resourcesPath, "ollama")
  : path.resolve(process.cwd(), "..", "verbis");

export async function connector_init(connector_name: string) {
  try {
    const response = await axios.get(
      `http://localhost:8081/connectors/${connector_name}/init`
    );
    console.log("Google Init Response:", response.data);
    // Additional logic based on response
    return response.data["id"];
  } catch (error) {
    console.error("Error in Google Init:", error);
    throw error; // Rethrow or handle as needed
  }
}

export async function connector_auth_setup(connector_id: string) {
  try {
    const response = await axios.get(
      `http://localhost:8081/connectors/${connector_id}/auth_setup`
    );
    console.log("Connector Auth Setup Response:", response.data);
    // Additional logic based on response
  } catch (error) {
    console.error("Error in Connector Auth Setup:", error);
    throw error; // Rethrow or handle as needed
  }
}

export async function force_sync() {
  try {
    const response = await axios.get("http://localhost:8081/sync/force");
    console.log("Force Sync Response:", response.data);
    // Additional logic based on response
  } catch (error) {
    console.error("Error in Force Sync:", error);
    throw error; // Rethrow or handle as needed
  }
}

async function* responseGenerator(
  response: Response
): AsyncGenerator<GenerateChunk, void, undefined> {
  const reader = response.body!.getReader();
  const textDecoder = new TextDecoder();
  let buffer = "";
  let isFirst = true;

  while (true) {
    const { done, value } = await reader.read();
    if (done) break;

    buffer += textDecoder.decode(value, { stream: true });
    let boundary;

    while ((boundary = buffer.indexOf("\n")) >= 0) {
      const jsonStr = buffer.substring(0, boundary);
      buffer = buffer.substring(boundary + 1);
      if (!/\S/.test(jsonStr) || jsonStr.length == 0) {
        // Skip whitespace lines
        continue;
      }

      try {
        const obj = JSON.parse(jsonStr);
        if (isFirst) {
          isFirst = false;
          yield { content: "", sources: obj.sources };
        } else {
          if (obj.done) return; // Exit if the stream indicates completion
          yield { content: obj.message.content, sources: [] };
        }
      } catch (error) {
        console.error("Failed to parse JSON:", error);
      }
    }
  }
}

// StreamedResponse is the type of each chunk returned by ollama
interface StreamedResponse {
  done: boolean;
  response: string;
}
interface GenerateChunk {
  content: string;
  sources: ResultSource[]; // Only available in the first result while streaming
}
interface HistoryItem {
  role: string;
  content: string;
}

export async function generate(
  promptText: string,
  conversation_id: string,
): Promise<{
  sources: ResultSource[];
  generator: AsyncGenerator<GenerateChunk, void, unknown>;
}> {
  const payload = {
    prompt: promptText,
  };

  const controller = new AbortController();
  const response = await fetch(`http://localhost:8081/conversations/${conversation_id}/prompt`, {
    method: "POST",
    headers: {
      "Content-Type": "application/json",
    },
    body: JSON.stringify(payload),
    signal: controller.signal,
  });

  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);

  const generator = responseGenerator(response);
  const initialResponse = await generator.next();
  let sources: ResultSource[];
  if (
    !initialResponse.done &&
    initialResponse.value &&
    "sources" in initialResponse.value
  ) {
    sources = initialResponse.value.sources; // Now TypeScript knows 'urls' exists
  }

  return { sources: sources, generator: generator };
}

export async function list_connectors() {
  try {
    const response = await axios.get("http://localhost:8081/connectors");
    return response.data;
  } catch (error) {
    console.error("Connector list", error);
    throw error;
  }
}

export async function list_conversations() {
  try {
    const response = await axios.get("http://localhost:8081/conversations");
    return response.data;
  } catch (error) {
    console.error("Conversation list", error);
    throw error;
  }
}

export async function create_conversation() {
  try {
    const response = await axios.post("http://localhost:8081/conversations");
    console.log("Create Conversation Response:", response.data);
    return response.data.id;
  } catch (error) {
    console.error("Error in Create Conversation:", error);
    throw error; // Rethrow or handle as needed
  }
}
