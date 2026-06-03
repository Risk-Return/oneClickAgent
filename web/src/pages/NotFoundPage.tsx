import { Link } from "react-router-dom";
import { useTranslation } from "react-i18next";
import { Button } from "@/components/ui/button";

export function NotFoundPage() {
  const { t } = useTranslation();
  return (
    <div className="flex min-h-screen flex-col items-center justify-center gap-4">
      <h1 className="text-6xl font-bold">{t("notFound.title")}</h1>
      <p className="text-lg text-muted-foreground">{t("notFound.message")}</p>
      <Button asChild>
        <Link to="/">{t("notFound.backToDashboard")}</Link>
      </Button>
    </div>
  );
}
