# ChatGPT tui README

My first ever terminal UI! Everything is stored locally on sqlite and written in Go!

## Technologies

- SQLite
- [bubbletea](https://github.com/charmbracelet/bubbletea)
- Go

## Installation

Please make sure that you expose a `CHAT_GPT_API_KEY` inside of your environment; we require it to make api calls!

Set up your [api key](https://platform.openai.com/api-keys)

```bash
export CHAT_GPT_API_KEY="some-key" # you would want to export this in your .zshrc
brew tap tearingitup786/tearingitup786
brew install chatgpt-tui
chatgpt-tui
```

To get access to the release candidates, install command:

```bash
brew install rc-chatgpt-tui
rc-chatgpt-tui
```

## Config

We provide a `config.json` file within your directory for easy access to essential settings.
On most Macs, the path is `~/.chatgpt-tui/config.json`.
This file includes the URL used for network calls to the TUI,
specified as `chatGPTAPiUrl: "https://api.openai.com/v1/chat/completions"`.
Additionally, the `systemMessage` field is available for customizing system prompt messages.

## Demo

![tui demo](./tui-demo.gif)

## Global Keybindings

- `Tab`: \*Change focus between panes. The currently focused pane will be highlighted with a pink border.
  - You can only change focus if Prompt Pane is not in `insert mode`
- `Ctrl+o`: Toggles zen mode
- `Ctrl+c`: Exit the program

## Prompt Pane

- `i`: Enters insert mode (you can now safely paste messages into the tui)
- `esc`: Exit insert mode for the prompt

## Chat Messages Pane

- `y`: Copies the last message from ChatGPT into your clipboard.
- `Y`: Copies all messages from the ChatGPT session into your clipboard.

## Settings Pane

- `m`: Opens an input dialog to change the model.
- `f`: Opens an input dialog to change the frequency of updates.
- `t`: Opens an input dialog to set the maximum number of tokens per message.

## Sessions Pane

- `Ctrl+N`: Creates a new session.
- `d`: Deletes the currently selected session from the list.
- `Enter`: Switches to the session that is currently selected.

Please refer to this guide as you navigate the TUI. Happy exploring!

### Dev notes

The SQL db is stored in you `your/home/directory/.chatgpt-tui`, as well as the debug log. To enable `debug` mode, `export DEBUG=1` before running the program.
