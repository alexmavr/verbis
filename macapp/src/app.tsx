import { useState } from 'react'
import Store from 'electron-store'
import { getCurrentWindow, app } from '@electron/remote'

import { google_init, google_sync, generate } from './client'
import LamoidIcon from './lamoid.svg'

const store = new Store()

enum Step {
  WELCOME = 0,
  GOOGLE_INIT,
  GOOGLE_SYNC,
  PROMPT,
}

export default function () {
  const [step, setStep] = useState<Step>(Step.WELCOME)
  const [promptText, setPromptText] = useState(''); // State to store input from the textbox
  const [promptResponse, setPromptResponse] = useState(''); // State to store the prompt response

  return (
    <div className='drag'>
      <div className='mx-auto flex min-h-screen w-full flex-col justify-between bg-white px-4 pt-16'>
        {step === Step.WELCOME && (
          // TODO: Wait for all processes to start successfully, show spinner
          // Block on a /health endpoint

          <>
            <div className='mx-auto text-center'>
              <h1 className='mb-6 mt-4 text-2xl tracking-tight text-gray-900'>Welcome to Lamoid</h1>
              <p className='mx-auto w-[65%] text-sm text-gray-400'>
                Let's get you up and running.
              </p>
              <button
                onClick={() => setStep(Step.GOOGLE_INIT)}
                className='no-drag rounded-dm mx-auto my-8 w-[40%] rounded-md bg-black px-4 py-2 text-sm text-white hover:brightness-110'
              >
                Google sync
              </button>
            </div>
            <div className='mx-auto'>
              <LamoidIcon />
            </div>
          </>
        )}
        {step === Step.GOOGLE_INIT && (
          <>
            <div className='mx-auto flex flex-col space-y-28 text-center'>
              <h1 className='mt-4 text-2xl tracking-tight text-gray-900'>Set up your google connector</h1>
              <div className='mx-auto'>
                <button
                  onClick={async () => {
                    try {
                      await google_init()
                      setStep(Step.GOOGLE_SYNC)
                    } catch (e) {
                      console.error('could not install: ', e)
                    } finally {
                      getCurrentWindow().show()
                      getCurrentWindow().focus()
                    }
                  }}
                  className='no-drag rounded-dm mx-auto w-[60%] rounded-md bg-black px-4 py-2 text-sm text-white hover:brightness-110'
                >
                  Configure google OAuth
                </button>
                <p className='mx-auto my-4 w-[70%] text-xs text-gray-400'>
                  Your browser will open to configure the OAuth credentials.
                </p>
              </div>
            </div>
          </>
        )}
        {step === Step.GOOGLE_SYNC && (
          <>
            <div className='mx-auto flex flex-col space-y-20 text-center'>
              <h1 className='mt-4 text-2xl tracking-tight text-gray-900'>Sync data from your google account</h1>
              <div className='flex flex-col'>
                <div className='group relative flex items-center'>
                  <button
                    onClick={async () => {
                      try {
                        await google_sync()
                        setStep(Step.PROMPT)
                      } catch (e) {
                        console.error('could not install: ', e)
                      } finally {
                        getCurrentWindow().show()
                        getCurrentWindow().focus()
                      }
                    }}
                    className='no-drag rounded-dm mx-auto w-[60%] rounded-md bg-black px-4 py-2 text-sm text-white hover:brightness-110'
                  >
                    Sync from Google
                  </button>
                </div>
              </div>
            </div>
          </>
        )}
        {step === Step.PROMPT && (
          <>
            <div className='mx-auto flex flex-col space-y-20 text-center'>
              <h1 className='mt-4 text-2xl tracking-tight text-gray-900'>Prompt to your heart's desire</h1>
              <div className='flex flex-col'>
                <input
                  type="text"
                  value={promptText}
                  onChange={e => setPromptText(e.target.value)}
                  placeholder="Enter your prompt"
                  className="text-center w-full p-2 border border-gray-300 rounded-md focus:outline-none focus:ring-2 focus:ring-black focus:border-transparent"
                />
                <div className='group relative flex items-center'>
                  <button
                    onClick={async () => {
                      try {
                        const response = await generate(promptText)
                        console.log('Prompt response:', response)
                        setPromptResponse(response); // Store the response to state
                      } catch (e) {
                        console.error('could not prompt: ', e)
                      } finally {
                        getCurrentWindow().show()
                        getCurrentWindow().focus()
                      }
                    }}
                    className='no-drag rounded-dm mx-auto w-[60%] rounded-md bg-black px-4 py-2 text-sm text-white hover:brightness-110'
                  >
                    Prompt
                  </button>
                  {promptResponse && (
                    <div className="mt-4 text-sm text-gray-600">
                      Response: {promptResponse}
                    </div>
                  )}
                </div>
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
