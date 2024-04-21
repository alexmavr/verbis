import { useState } from 'react'
import copy from 'copy-to-clipboard'
import { CheckIcon, DocumentDuplicateIcon } from '@heroicons/react/24/outline'
import Store from 'electron-store'
import { getCurrentWindow, app } from '@electron/remote'

import { google_init, google_sync, prompt } from './install'
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
  const [commandCopied, setCommandCopied] = useState<boolean>(false)

  const command = 'ollama run llama2'

  return (
    <div className='drag'>
      <div className='mx-auto flex min-h-screen w-full flex-col justify-between bg-white px-4 pt-16'>
        {step === Step.WELCOME && (
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
                <div className='group relative flex items-center'>
                  <button
                    onClick={async () => {
                      try {
                        await prompt("nikolas")
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
                    Prompt
                  </button>
                </div>
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
