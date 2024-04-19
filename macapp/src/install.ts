import * as fs from 'fs'
import { exec as cbExec } from 'child_process'
import * as path from 'path'
import { promisify } from 'util'

const app = process && process.type === 'renderer' ? require('@electron/remote').app : require('electron').app
const lamoid = app.isPackaged ? path.join(process.resourcesPath, 'ollama') : path.resolve(process.cwd(), '..', 'lamoid')
const exec = promisify(cbExec)
const symlinkPath = '/usr/local/bin/ollama'

export async function google_init() {
}

export async function google_sync() {
}

export async function prompt(query: string) {
}
