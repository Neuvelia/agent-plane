import { renderToStaticMarkup } from "react-dom/server";
import { afterEach, describe, expect, it, vi } from "vitest";
import App from "../src/App";
import { WELCOME_KEY } from "../src/capabilities";
import { I18nProvider } from "../src/i18n/context";

const launch = () =>
  renderToStaticMarkup(
    <I18nProvider locale="en">
      <App />
    </I18nProvider>,
  );

// Static render runs no effects, so the screen is the one chosen at first paint.
const storage = (s: unknown) => {
  vi.stubGlobal("window", { agentplane: { appVersion: "1.2.3" } });
  vi.stubGlobal("localStorage", s);
};

afterEach(() => vi.unstubAllGlobals());

describe("first launch (F2.11 requirement 10)", () => {
  it("opens the welcome screen with empty storage", () => {
    storage({ getItem: () => null, setItem: () => undefined });
    expect(launch()).toContain('data-testid="welcome"');
  });
  it("opens the welcome screen when storage throws", () => {
    storage({
      getItem: () => {
        throw new Error("denied");
      },
      setItem: () => {
        throw new Error("denied");
      },
    });
    expect(launch()).toContain('data-testid="welcome"');
  });
  it("opens the default screen, the Inbox, once Skip has been recorded", () => {
    storage({
      getItem: (k: string) => (k === WELCOME_KEY ? "done" : null),
      setItem: () => undefined,
    });
    const out = launch();
    expect(out).not.toContain('data-testid="welcome"');
    expect(out).toContain('data-testid="inbox"');
  });
});
