// Single import point for all Wails-bound Go methods and model types.
// Components should import from here, never from wailsjs/ directly.

export {
  ActivateTestAccount,
  CheckForUpdate,
  ClearAllLocalData,
  DeleteAPIKey,
  DeleteQuestionSet,
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
  InstallUpdate,
  ListAvailableModels,
  ListCompanies,
  ListCompanyProblems,
  ListDisplays,
  ListQuestionSets,
  ListSessions,
  ListStarredCompanies,
  ListVoices,
  MinimiseWindow,
  OpenAccessibilitySettings,
  OpenReleasePage,
  OpenURL,
  PreviewVoice,
  QuitApp,
  RequestTestCode,
  RetryHotkey,
  RevealDatabaseFile,
  SaveQuestionSet,
  SendMessage,
  SetAPIKey,
  SetCaptureRegion,
  SetCompanyStarred,
  SetOverlayExpanded,
  SignOutTestAccount,
  SnapshotDisplay,
  StartCapture,
  StartCompanySession,
  StartMockInterview,
  StartSession,
  StartSetMockInterview,
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
