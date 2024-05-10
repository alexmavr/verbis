import * as fs from 'fs'
import * as path from 'path'
import { promisify } from 'util'
import axios from 'axios';
import { json } from 'stream/consumers';

const app = process && process.type === 'renderer' ? require('@electron/remote').app : require('electron').app
const lamoid = app.isPackaged ? path.join(process.resourcesPath, 'ollama') : path.resolve(process.cwd(), '..', 'lamoid')

export async function google_init() {
  try {
    const response = await axios.get('http://localhost:8081/connectors/googledrive/init');
    console.log('Google Init Response:', response.data);
    // Additional logic based on response
  } catch (error) {
    console.error('Error in Google Init:', error);
    throw error; // Rethrow or handle as needed
  }
}

export async function google_auth_setup() {
  try {
    const response = await axios.get('http://localhost:8081/connectors/googledrive/auth_setup');
    console.log('Google Auth Setup Response:', response.data);
    // Additional logic based on response
  } catch (error) {
    console.error('Error in Google Auth Setup:', error);
    throw error; // Rethrow or handle as needed
  }
}


export async function google_sync() {
  try {
    const response = await axios.get('http://localhost:8081/sync/force');
    console.log('Force Sync Response:', response.data);
    // Additional logic based on response
  } catch (error) {
    console.error('Error in Force Sync:', error);
    throw error; // Rethrow or handle as needed
  }
}

async function* responseGenerator(response: Response): AsyncGenerator<GenerateChunk, void, undefined> {
  const reader = response.body!.getReader();
  const textDecoder = new TextDecoder();
  let buffer = '';
  let isFirst = true;

  while (true) {
      const { done, value } = await reader.read();
      if (done) break;

      buffer += textDecoder.decode(value, { stream: true });
      let boundary;

      while ((boundary = buffer.indexOf('\n')) >= 0) {
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
                  yield { content: "", urls: obj.urls};
              } else {
                  if (obj.done) return;  // Exit if the stream indicates completion
                  yield { content: obj.message.content, urls: []};
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
// Returned by the generate function as a generator
interface GenerateChunk {
  content: string;
  urls: string[]; // Only populated on the first message
}
interface HistoryItem {
  role: string;
  content: string;
}

export async function generate(promptText: string, history: HistoryItem[] = []): Promise<{ initialUrls: string[], generator: AsyncGenerator<GenerateChunk, void, unknown> }> {
  const payload = {
    prompt: promptText,
    history: history.length > 0 ? history : [],
  };

  const controller = new AbortController();
  const response = await fetch("http://localhost:8081/prompt", {
    method: "POST",
    headers: {
      'Content-Type': 'application/json',
    },
    body: JSON.stringify(payload),
    signal: controller.signal
  });

  if (!response.ok) throw new Error(`HTTP error! status: ${response.status}`);


  const generator = responseGenerator(response);
  const initialResponse = await generator.next();
  let urls: string[];
  if (!initialResponse.done && initialResponse.value && 'urls' in initialResponse.value) {
    urls = initialResponse.value.urls;  // Now TypeScript knows 'urls' exists
  }

  return { initialUrls: urls, generator: generator};
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