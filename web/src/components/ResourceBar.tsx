export function ResourceBar({ label, used, total, unit = "MB" }: { label: string; used: number; total: number; unit?: string }) {
  const percent = total > 0 ? Math.min((used / total) * 100, 100) : 0;
  const getColor = () => {
    if (percent > 90) return "bg-red-500";
    if (percent > 70) return "bg-amber-500";
    return "bg-emerald-500";
  };

  return (
    <div className="space-y-1">
      <div className="flex justify-between text-xs">
        <span className="text-muted-foreground">{label}</span>
        <span className="font-medium">
          {used}/{total} {unit}
        </span>
      </div>
      <div className="h-2 rounded-full bg-secondary">
        <div
          className={`h-2 rounded-full transition-all ${getColor()}`}
          style={{ width: `${percent}%` }}
        />
      </div>
    </div>
  );
}
