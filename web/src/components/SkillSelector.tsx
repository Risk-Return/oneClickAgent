import { Label } from "@/components/ui/label";
import type { Skill } from "@/api/schemas";

interface SkillSelectorProps {
  skills: Skill[];
  selectedSkillId: string | null;
  onSkillChange: (skillId: string | null) => void;
  installedSkillIds?: string[];
}

export function SkillSelector({ skills, selectedSkillId, onSkillChange, installedSkillIds }: SkillSelectorProps) {
  return (
    <div className="space-y-3">
      <Label>Skill (optional, at most one)</Label>
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          onClick={() => onSkillChange(null)}
          className={`inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-sm font-medium transition-colors ${
            selectedSkillId === null
              ? "border-primary bg-primary text-primary-foreground"
              : "border-muted-foreground/25 bg-transparent hover:bg-accent"
          }`}
        >
          None
        </button>
        {skills.map((skill) => {
          const isInstalled = !installedSkillIds || installedSkillIds.includes(skill.id);
          const isSelected = selectedSkillId === skill.id;

          return (
            <button
              key={skill.id}
              type="button"
              onClick={() => isInstalled && onSkillChange(skill.id)}
              disabled={!isInstalled}
              className={`inline-flex items-center gap-1.5 rounded-full border px-3 py-1.5 text-sm font-medium transition-colors ${
                isSelected
                  ? "border-primary bg-primary text-primary-foreground"
                  : isInstalled
                    ? "border-muted-foreground/25 bg-transparent hover:bg-accent"
                    : "cursor-not-allowed border-muted-foreground/10 opacity-50"
              }`}
            >
              {skill.name}
              {!isInstalled && <span className="text-xs opacity-70">unavailable</span>}
            </button>
          );
        })}
      </div>
    </div>
  );
}
