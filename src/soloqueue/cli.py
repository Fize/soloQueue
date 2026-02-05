import asyncio
import sys

from langchain_core.messages import HumanMessage, AIMessage, ToolMessage
from soloqueue.core.logger import setup_logger, logger
from soloqueue.orchestration.graph.builder import build_dynamic_graph

def print_stream(event: dict):
    """
    Pretty print graph events.
    Key is node name, value is state update.
    """
    for node_name, update in event.items():
        if "messages" in update:
            last_msg = update["messages"][-1]
            if isinstance(last_msg, AIMessage):
                content = last_msg.content
                tool_calls = last_msg.tool_calls
                
                print(f"\n\033[94m[{node_name}]\033[0m") # Blue
                if content:
                    print(f"{content}")
                if tool_calls:
                    for tc in tool_calls:
                        print(f"\033[93m🛠️  Tool Call: {tc['name']}({tc['args']})\033[0m")
                        
            elif isinstance(last_msg, ToolMessage):
                print(f"\n\033[90m[{node_name} Output]\033[0m") # Grey
                print(f"{last_msg.content[:200]}..." if len(last_msg.content) > 200 else last_msg.content)

async def main():
    print("🚀 Initializing SoloQueue...")
    setup_logger()
    
    try:
        graph = build_dynamic_graph()
        print("✅ Graph constructed successfully.")
        
        # Initial greeting
        print("\n\033[1;32mSoloQueue Ready. Type 'exit' to quit.\033[0m")
        
        while True:
            try:
                user_input = input("\n\033[1mUSER > \033[0m").strip()
                if not user_input:
                    continue
                if user_input.lower() in ["exit", "quit"]:
                    break
                    
                # Stream the graph execution
                # We inject the HumanMessage as input
                input_state = {"messages": [HumanMessage(content=user_input)]}
                
                async for event in graph.astream(input_state):
                    print_stream(event)
                    
            except KeyboardInterrupt:
                print("\nInterrupted.")
                break
            except Exception as e:
                logger.error(f"Runtime Error: {e}")
                print(f"\033[91mError: {e}\033[0m")
                
    except Exception as e:
        logger.critical(f"Startup Failed: {e}")
        print(f"Startup Failed: {e}")
        sys.exit(1)

if __name__ == "__main__":
    asyncio.run(main())
