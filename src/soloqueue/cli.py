import asyncio
import sys
import os

from langchain_core.messages import HumanMessage
from soloqueue.core.logger import setup_logger, logger
from soloqueue.orchestration.graph.builder import build_dynamic_graph
from langgraph.checkpoint.sqlite.aio import AsyncSqliteSaver

async def main():
    print("🚀 Initializing SoloQueue...")
    setup_logger()
    
    # Ensure persistence directory
    os.makedirs(".soloqueue", exist_ok=True)
    
    try:
        # Initialize Checkpointer
        async with AsyncSqliteSaver.from_conn_string(".soloqueue/state.db") as checkpointer:
            graph = build_dynamic_graph(checkpointer=checkpointer)
            print("✅ Graph constructed successfully.")
            print("\n\033[1;32mSoloQueue Ready. Type 'exit' to quit.\033[0m")
            
            # Use a fixed thread ID for the CLI session 
            # (In a real app, user might switch threads, but here we keep context)
            config = {"configurable": {"thread_id": "cli_session_1"}}
            
            while True:
                try:
                    user_input = input("\n\033[1mUSER > \033[0m").strip()
                    if not user_input:
                        continue
                    if user_input.lower() in ["exit", "quit"]:
                        break
                        
                    input_state = {"messages": [HumanMessage(content=user_input)]}
                    
                    # Streaming State
                    current_node = None
                    is_thinking = False
                    
                    async for event in graph.astream_events(input_state, config=config, version="v1"):
                        kind = event["event"]
                        
                        if kind == "on_chain_start":
                            pass

                        # 1. Node Entry Detection
                        tags = event.get("tags", [])
                        if "langgraph:node" in tags:
                            meta = event.get("metadata", {})
                            node = meta.get("langgraph_node", "")
                            if node and node != current_node:
                                current_node = node
                                print(f"\n\033[94m[{current_node}]\033[0m") # Blue

                        # 2. LLM Streaming
                        if kind == "on_chat_model_stream":
                            chunk = event["data"]["chunk"]
                            
                            # DeepSeek Native Reasoning Support
                            reasoning = chunk.additional_kwargs.get("reasoning_content", "")
                            content = chunk.content
                            
                            # Logic for native reasoning field
                            if reasoning:
                                if not is_thinking:
                                    print("\n\033[90m💭 [Thinking] ", end="", flush=True)
                                    is_thinking = True
                                print(reasoning, end="", flush=True)
                                continue # Skip content processing if this is a purely reasoning chunk

                            # If we were thinking (native) and now get content (and no reasoning), close it
                            if is_thinking and content:
                                print("\033[0m\n", end="", flush=True)
                                is_thinking = False

                            if content:
                                to_print = content
                                
                                # Fallback: Check for embedded <think> tags (Ollama/Proxies)
                                if "<think>" in to_print:
                                    print("\n\033[90m", end="", flush=True) # Start Grey
                                    is_thinking = True
                                    to_print = to_print.replace("<think>", "💭 [Thinking] ")
                                
                                if "</think>" in to_print:
                                    parts = to_print.split("</think>")
                                    print(parts[0], end="", flush=True)
                                    print("\033[0m\n", end="", flush=True)
                                    is_thinking = False
                                    if len(parts) > 1:
                                        print(parts[1], end="", flush=True)
                                    continue

                                print(to_print, end="", flush=True)

                        # 3. Tool Execution
                        elif kind == "on_tool_start":
                            print(f"\n\033[93m🛠️  Tool Call: {event['name']}...\033[0m", end="")
                        
                        elif kind == "on_tool_end":
                            output = str(event['data'].get('output'))
                            preview = output[:100] + "..." if len(output) > 100 else output
                            print(f" \033[90m-> {preview}\033[0m")

                except KeyboardInterrupt:
                    print("\nInterrupted.")
                    break
                except Exception as e:
                    logger.error(f"Runtime Error: {e}")
                    print(f"\033[91mError: {e}\033[0m")
                    # import traceback
                    # traceback.print_exc()

    except Exception as e:
        logger.critical(f"Startup Failed: {e}")
        print(f"Startup Failed: {e}")
        sys.exit(1)

if __name__ == "__main__":
    asyncio.run(main())
