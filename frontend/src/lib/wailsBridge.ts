// Single import point for all Wails-bound Go methods and model types.
// Components should import from here, never from wailsjs/ directly.

export {
  CheckForUpdate,
  DeleteAPIKey,
  DeleteSession,
  EndSession,
  EnterOverlayMode,
  ExitOverlayMode,
  GetAppVersion,
  GetAuthStatus,
  GetDebrief,
  GetHotkeyStatus,
  GetLatestScreenshot,
  GetPreferences,
  GetSessionTranscript,
  ListAvailableModels,
  ListCompanies,
  ListCompanyProblems,
  ListDisplays,
  ListSessions,
  ListVoices,
  MinimiseWindow,
  OpenInputMonitoringSettings,
  OpenReleasePage,
  OpenURL,
  PreviewVoice,
  QuitApp,
  SendMessage,
  SetAPIKey,
  SetCaptureRegion,
  SetOverlayExpanded,
  SnapshotDisplay,
  StartCapture,
  StartCompanySession,
  StartMockInterview,
  StartSession,
  StopCapture,
  SynthesizeSpeech,
  ToggleMaximiseWindow,
  TranscribeAudio,
  UpdatePreferences,
} from "../../wailsjs/go/main/App";

export { models, capture, hotkey } from "../../wailsjs/go/models";

// Wails runtime event bus — used for backend-pushed events (e.g. the global
// voice-hotkey "ptt:down"). Re-exported here so components keep a single import
// point and never reach into wailsjs/ directly.
export { EventsOn } from "../../wailsjs/runtime";
