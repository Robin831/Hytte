category: Added
- **AI prompts table** - Added `ai_prompts` table to the schema as a shared foundation for customizable Claude prompt templates. Seeds default instruction strings for the `analysis`, `comparison`, and `training_load` prompt keys. Existing customizations are preserved on upgrade via `INSERT OR IGNORE`. (Hytte-434x)
