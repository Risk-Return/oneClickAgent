declare module "@novnc/novnc/core/rfb" {
  interface RFBOptions {
    credentials?: { password: string };
    wsProtocols?: string[];
  }

  export default class RFB {
    constructor(target: HTMLElement, url: string, options?: RFBOptions);
    addEventListener(event: string, callback: () => void): void;
    removeEventListener(event: string, callback: () => void): void;
    disconnect(): void;
    scaleViewport: boolean;
    resizeSession: boolean;
    viewOnly: boolean;
    qualityLevel: number;
  }
}
