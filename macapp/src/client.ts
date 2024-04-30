import * as fs from 'fs'
import * as path from 'path'
import { promisify } from 'util'
import axios from 'axios';

const app = process && process.type === 'renderer' ? require('@electron/remote').app : require('electron').app
const lamoid = app.isPackaged ? path.join(process.resourcesPath, 'ollama') : path.resolve(process.cwd(), '..', 'lamoid')

export async function google_init() {
  try {
    const response = await axios.get('http://localhost:8081/connectors/google/init');
    console.log('Google Init Response:', response.data);
    // Additional logic based on response
  } catch (error) {
    console.error('Error in Google Init:', error);
    throw error; // Rethrow or handle as needed
  }
}

export async function google_auth_setup() {
  try {
    const response = await axios.get('http://localhost:8081/connectors/google/auth_setup');
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
interface PromptResponse {
   content: string;
   sourceURLs: string[];
}

export async function generate(promptText: string, history: { role: string; content: string; }[] = []): Promise<PromptResponse> {
  try {
    const payload = {
      prompt: promptText,
      history: history.length > 0 ? history : []
    };

    const response = await axios.post('http://localhost:8081/prompt', payload);
    console.log('Prompt Response:', response.data);

    const { content, sourceURLs } = response.data;
    return { content, sourceURLs };
  } catch (error) {
    console.error('Error when sending prompt:', error);
    throw new Error(`Failed to retrieve data: ${error.message}`);
  }
}