import { createContext, useContext, useState, type ReactNode } from 'react';
import type { WalkInput, WalkResult } from './types';

type Draft = { minutes: string; budget: string; pace: WalkInput['pace']; categories: string[]; useGPS: boolean; distance: string; stops: string; mode: 'walking'|'cycling' };
type Calculation = { request: WalkInput; result: WalkResult };
type WalkContextValue = {
  draft: Draft;
  setDraft: (draft: Draft) => void;
  calculation: Calculation | null;
  setCalculation: (calculation: Calculation) => void;
};
const WalkContext = createContext<WalkContextValue | null>(null);
export function WalkProvider({ children }: { children: ReactNode }) {
  const [draft, setDraft] = useState<Draft>({ minutes: '90', budget: '', pace: 'normal', categories: [], useGPS: false, distance: '', stops: '', mode: 'walking' });
  const [calculation, setCalculation] = useState<Calculation | null>(null);
  return <WalkContext.Provider value={{ draft, setDraft, calculation, setCalculation }}>{children}</WalkContext.Provider>;
}
export function useWalkPlanner() {
  const context = useContext(WalkContext);
  if (!context) throw new Error('WalkProvider is missing');
  return context;
}
