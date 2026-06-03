import { useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import RFB from "@novnc/novnc/core/rfb";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Loader2, Monitor, Save, X } from "lucide-react";

interface VNCPanelProps {
  open: boolean;
  onClose: () => void;
  wsUrl: string;
  rfbPassword: string;
  sessionId: string;
  onSaveLogin: (sessionId: string, label: string) => Promise<void>;
}

type ConnectionStatus = "connecting" | "connected" | "disconnected" | "error";

export function VNCPanel({ open, onClose, wsUrl, rfbPassword, sessionId, onSaveLogin }: VNCPanelProps) {
  const { t } = useTranslation();
  const canvasRef = useRef<HTMLDivElement>(null);
  const rfbRef = useRef<RFB | null>(null);
  const [status, setStatus] = useState<ConnectionStatus>("connecting");
  const [showSaveLogin, setShowSaveLogin] = useState(false);
  const [loginLabel, setLoginLabel] = useState("");
  const [saving, setSaving] = useState(false);
  const initializedRef = useRef(false);

  useEffect(() => {
    if (!open || !canvasRef.current || initializedRef.current) return;
    initializedRef.current = true;

    setStatus("connecting");

    try {
      const rfb = new RFB(canvasRef.current, wsUrl, {
        credentials: { password: rfbPassword },
        wsProtocols: ["binary"],
      });

      rfb.addEventListener("connect", () => setStatus("connected"));
      rfb.addEventListener("disconnect", () => setStatus("disconnected"));
      rfb.addEventListener("credentialsrequired", () => setStatus("error"));
      rfb.scaleViewport = true;
      rfb.resizeSession = true;

      rfbRef.current = rfb;
    } catch {
      setStatus("error");
    }

    return () => {
      if (rfbRef.current) {
        rfbRef.current.disconnect();
        rfbRef.current = null;
      }
      initializedRef.current = false;
    };
  }, [open, wsUrl, rfbPassword]);

  const handleClose = () => {
    if (rfbRef.current) {
      rfbRef.current.disconnect();
      rfbRef.current = null;
    }
    initializedRef.current = false;
    setStatus("connecting");
    setShowSaveLogin(false);
    onClose();
  };

  const handleSaveLogin = async () => {
    if (!loginLabel.trim()) return;
    setSaving(true);
    try {
      await onSaveLogin(sessionId, loginLabel.trim());
      setLoginLabel("");
      setShowSaveLogin(false);
    } finally {
      setSaving(false);
    }
  };

  const statusBadgeVariant: Record<ConnectionStatus, "warning" | "success" | "destructive" | "secondary"> = {
    connecting: "warning",
    connected: "success",
    disconnected: "secondary",
    error: "destructive",
  };

  const statusLabel: Record<ConnectionStatus, string> = {
    connecting: t("vnc.connecting"),
    connected: t("vnc.live"),
    disconnected: t("vnc.disconnected"),
    error: t("vnc.error"),
  };

  const currentVariant = statusBadgeVariant[status];

  return (
    <Dialog open={open} onOpenChange={(o) => !o && handleClose()}>
      <DialogContent className="max-w-4xl max-h-[90vh]">
        <DialogHeader className="flex flex-row items-center justify-between">
          <div className="flex items-center gap-3">
            <DialogTitle>
              <Monitor className="inline mr-2 h-5 w-5" />
              {t("vnc.browserControl")}
            </DialogTitle>
            <Badge variant={currentVariant}>{statusLabel[status]}</Badge>
          </div>
        </DialogHeader>

        <div className="relative bg-black rounded-md overflow-hidden" style={{ height: "60vh" }}>
          <div
            ref={canvasRef}
            className="w-full h-full"
          />
        </div>

        <div className="flex items-center justify-between">
          <div>
            {!showSaveLogin ? (
              <Button
                variant="outline"
                size="sm"
                onClick={() => setShowSaveLogin(true)}
                disabled={status !== "connected"}
              >
                <Save className="mr-2 h-4 w-4" /> {t("vnc.saveLogin")}
              </Button>
            ) : (
              <div className="flex items-center gap-2">
                <Input
                  value={loginLabel}
                  onChange={(e) => setLoginLabel(e.target.value)}
                  placeholder={t("vnc.loginLabel")}
                  className="h-8 w-48"
                  autoFocus
                  onKeyDown={(e) => e.key === "Enter" && handleSaveLogin()}
                />
                <Button size="sm" onClick={handleSaveLogin} disabled={!loginLabel.trim() || saving}>
                  {saving ? <Loader2 className="mr-1 h-3 w-3 animate-spin" /> : null}
                  {t("vnc.save")}
                </Button>
                <Button variant="ghost" size="sm" onClick={() => setShowSaveLogin(false)}>
                  <X className="h-4 w-4" />
                </Button>
              </div>
            )}
          </div>
          <Button variant="ghost" size="sm" onClick={handleClose}>
            <X className="mr-2 h-4 w-4" /> {t("vnc.close")}
          </Button>
        </div>
      </DialogContent>
    </Dialog>
  );
}
