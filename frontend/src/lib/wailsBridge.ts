// Single import point for all Wails-bound Go methods and model types.
// Components should import from here, never from wailsjs/ directly.

export {
  EndSession,
  EnterOverlayMode,
  ExitOverlayMode,
  GetAuthStatus,
  GetLatestScreenshot,
  GetPreferences,
  GetSessionTranscript,
  ListAvailableModels,
  ListDisplays,
  ListSessions,
  ListVoices,
  SendMessage,
  SetAPIKey,
  SetCaptureRegion,
  SetOverlayExpanded,
  SnapshotDisplay,
  StartCapture,
  StartSession,
  StopCapture,
  SynthesizeSpeech,
  TranscribeAudio,
  UpdatePreferences,
} from "../../wailsjs/go/main/App";

export { models, capture } from "../../wailsjs/go/models";
