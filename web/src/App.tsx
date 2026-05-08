import { Board } from '@/components/Board';
import { TooltipProvider } from '@/components/ui/tooltip';

function App() {
  return (
    <TooltipProvider>
      <Board />
    </TooltipProvider>
  );
}

export default App;
