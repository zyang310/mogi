import { models } from "../../lib/wailsBridge";
import ModelPicker from "./ModelPicker";
import "./ModelsSection.css";

interface Props {
  authStatus: models.AuthStatus;
  prefs: models.Preferences | null;
  savePrefs: (patch: Partial<models.Preferences>, msg: string) => Promise<void>;
  // Deep-link to the API Keys section from the managed lock card (that pane
  // owns mode switching).
  onOpenApiKeys: () => void;
}

// ModelsSection is the Settings → Models pane: a thin wrapper that hosts the
// ModelPicker in a card and persists the selection through the shell's shared
// savePrefs. On a managed test account the model is pinned server-side, so the
// picker gives way to a static locked card — branching on authStatus (the
// backend's resolveModel enforces the pin regardless; this is mirror-only).
export default function ModelsSection({
  authStatus,
  prefs,
  savePrefs,
  onOpenApiKeys,
}: Props) {
  function saveModel(modelId: string) {
    return savePrefs({ model: modelId }, "Model saved.");
  }

  return (
    <>
      <header className="settings-head">
        <h1>Model Architecture</h1>
      </header>
      {authStatus.keyMode === "managed" ? (
        <div className="settings-card settings-card-placeholder">
          <span className="material-symbols-outlined">lock</span>
          <h3 className="settings-card-title">Model pinned by the test program</h3>
          {authStatus.pinnedModel && (
            <code className="models-pinned-id">{authStatus.pinnedModel}</code>
          )}
          <p className="settings-hint">
            Test accounts interview on one fixed model so feedback is comparable
            across testers. To pick your own, switch to your own keys under{" "}
            <button className="settings-link-btn" onClick={onOpenApiKeys}>
              API Keys
            </button>
            .
          </p>
        </div>
      ) : (
        // Flush layout: the picker floats in its own card like every
        // other section's content.
        <div className="settings-card">
          <ModelPicker currentModelId={prefs?.model ?? ""} onSelect={saveModel} />
        </div>
      )}
    </>
  );
}
