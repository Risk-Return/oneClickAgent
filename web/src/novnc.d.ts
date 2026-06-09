declare module "@novnc/novnc" {
  class RFB {
    constructor(canvas: HTMLElement, url: string | (() => string), options?: Record<string, unknown>);
    disconnect(): void;
    sendKey(keysym: number, code: string, down?: boolean): void;
    clipboardPasteFrom(text: string): void;
    addEventListener(event: string, handler: (...args: any[]) => void): void;
    removeEventListener(event: string, handler: (...args: any[]) => void): void;
    _sock?: WebSocket;
    viewOnly: boolean;
    scaleViewport: boolean;
    resizeSession: boolean;
    qualityLevel: number;
    compressionLevel: number;
  }
  export default RFB;
}
