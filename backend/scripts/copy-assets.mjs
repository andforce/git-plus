import { copyFileSync, mkdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';

const out = resolve('../dist/server/schema.sql');
mkdirSync(dirname(out), { recursive: true });
copyFileSync(resolve('../db/schema.sql'), out);
