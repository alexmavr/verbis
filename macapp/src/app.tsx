import { useState, useEffect } from 'react'
import Store from 'electron-store'
import { getCurrentWindow, app } from '@electron/remote'
import axios from 'axios'

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
  const [loading, setLoading] = useState(true); // State for the spinner
  const [conversation, setConversation] = useState([]);

  useEffect(() => {
    const checkHealth = async () => {
      try {
        await axios.get('http://localhost:8081/health');
        setLoading(false); // Turn off spinner on successful response
      } catch (error) {
        console.error('Error checking health: ', error);
        setTimeout(checkHealth, 3000); // Retry after 3 seconds if the request fails
      }
    };

    checkHealth();
  }, []);

  // Function to handle the prompting action
  const triggerPrompt = async () => {
    if (!promptText.trim()) return; // Do nothing if the prompt is empty

    // Transform conversation history to the expected format
    const history = conversation.map(item => ({
      role: item.role,
      content: item.content
    }));

    try {
      const response = await generate(promptText, history);
      // Assuming that response is just the assistant's text, adjust if it's structured differently
      setConversation(conv => [
        ...conv,
        { role: 'user', content: promptText },
        { role: 'assistant', content: response }
      ]);
      setPromptText(''); // Clear the input field after sending the prompt
    } catch (e) {
      console.error('Error during prompt generation: ', e);
      // You might want to handle this error in the UI as well
    } finally {
      getCurrentWindow().show();
      getCurrentWindow().focus();
    }
  };

  const renderConversation = () => {
    return conversation.map((item, index) => (
      <div key={index} className={`message ${item.role}`}>
        <div className="message-content">{item.content}</div>
      </div>
    ));
  };

  return (
    <div className='drag'>
      <div className='mx-auto flex min-h-screen w-full flex-col justify-between bg-white px-4 pt-16'>
        {step === Step.WELCOME && (
          <>
            <div className='mx-auto text-center'>
              <h1 className='mb-6 mt-4 text-2xl tracking-tight text-gray-900'>Welcome to Lamoid</h1>
              {loading ? (
                <div className="spinner">Lamoid is still starting...</div>
              ) : (
                <>
                  <p className='mx-auto w-[65%] text-sm text-gray-400'>
                    Let's get you up and running.
                  </p>
                  <button
                    onClick={() => setStep(Step.GOOGLE_INIT)}
                    className='no-drag rounded-dm mx-auto my-8 w-[40%] rounded-md bg-black px-4 py-2 text-sm text-white hover:brightness-110'
                  >
                    Google sync
                  </button>
                </>
              )}
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

              {/* Conversation history */}
              {conversation.length > 0 && (
                <div className='conversation-container'>
                  {conversation.map((item, index) => (
                    <div key={index} className={`message ${item.role}`}>
                      <div className="message-content">{item.content}</div>
                    </div>
                  ))}
                </div>
              )}

              {/* Prompt input and button */}
              <div className='prompt-section'>
                <input
                  type="text"
                  value={promptText}
                  onChange={e => setPromptText(e.target.value)}
                  placeholder="Enter your prompt"
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') {
                      e.preventDefault();
                      triggerPrompt();
                    }
                  }}
                  className="prompt-input"
                />
                <button
                  onClick={triggerPrompt}
                  className='prompt-button'
                >
                  Prompt
                </button>
              </div>
            </div>
          </>
        )}
      </div>
    </div>
  )
}
