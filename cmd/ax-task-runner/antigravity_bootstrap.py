# Copyright 2026 Google LLC
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

"""Prepare an AX workspace with the Antigravity agent.

Usage:
    antigravity_bootstrap.py --goal "<goal>" [--workspace DIR] [--data-dir DIR]

The agent is confined to the workspace directory: the process changes into it,
the agent's file tools are restricted to it, and agent state is kept outside it
in --data-dir when given.

The agent talks to Gemini by default. Set AX_MODEL_BASE_URL to the root of an
OpenAI-compatible completions API instead -- an in-cluster vLLM server, Ollama,
LM Studio -- and the agent uses that, with AX_MODEL_NAME naming the served model
and AX_MODEL_API_KEY carrying a key if the server wants one.

The caller (ax-task-runner) owns the overall timeout and only invokes this script
when a goal is set and a model endpoint is configured. Keys are read from the
environment only, never from a flag, so they do not show up in process listings.
Exits non-zero on failure so the caller can log it; the caller decides whether to
continue.
"""

import argparse
import asyncio
import logging
import os
import sys

logging.basicConfig(level=logging.INFO, format="%(asctime)s [%(levelname)s] %(message)s")

API_KEY_ENV = "GEMINI_API_KEY"
BASE_URL_ENV = "AX_MODEL_BASE_URL"
MODEL_NAME_ENV = "AX_MODEL_NAME"
OPENAI_API_KEY_ENV = "AX_MODEL_API_KEY"
DEFAULT_WORKSPACE = "/workspace"

SYSTEM_INSTRUCTIONS = (
    "You are the AX Antigravity workspace setup agent. Your job is to initialize the "
    "workspace at {workspace_dir} and prepare it for developer or task execution "
    "according to the declared goal. The current working directory is the workspace. "
    "Run all commands from there and only create or modify files inside it; do not touch "
    "paths outside the workspace."
)


def parse_args(argv: list[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        prog="antigravity_bootstrap.py",
        description="Prepare an AX workspace with the Antigravity agent.",
    )
    parser.add_argument(
        "--goal",
        default="",
        help="What the agent should set the workspace up for. If empty, the script exits without running the agent.",
    )
    parser.add_argument(
        "--workspace",
        default=DEFAULT_WORKSPACE,
        help=f"Workspace directory the agent operates in (default: {DEFAULT_WORKSPACE}).",
    )
    parser.add_argument(
        "--data-dir",
        default="",
        help="Where the agent keeps its own state, outside the workspace. Omit to use the SDK default.",
    )
    return parser.parse_args(argv)


async def run_bootstrap(goal: str, workspace_dir: str, data_dir: str) -> None:
    from google.antigravity import Agent, CapabilitiesConfig, LocalAgentConfig, LocalOpenAIAgentConfig
    from google.antigravity.hooks import policy

    config_kwargs = dict(
        workspaces=[workspace_dir],
        capabilities=CapabilitiesConfig(),
        # Deny file tools outside the workspace; allow everything else (including
        # run_command, which the SDK denies by default). Denies take priority.
        policies=[*policy.workspace_only([workspace_dir]), policy.allow_all()],
        system_instructions=SYSTEM_INSTRUCTIONS.format(workspace_dir=workspace_dir),
    )

    base_url = os.environ.get(BASE_URL_ENV, "")
    if base_url:
        config_cls = LocalOpenAIAgentConfig
        config_kwargs["base_url"] = base_url
        model = os.environ.get(MODEL_NAME_ENV, "")
        if model:
            config_kwargs["model"] = model
        # vLLM without --api-key serves unauthenticated requests, so a key here
        # is optional; the SDK's OpenAI client wants some value regardless.
        config_kwargs["api_key"] = os.environ.get(OPENAI_API_KEY_ENV) or "not-needed"
        logging.info("Using OpenAI-compatible endpoint %s (model=%s)", base_url, model or "<server default>")
    else:
        config_cls = LocalAgentConfig
        config_kwargs["api_key"] = os.environ[API_KEY_ENV]

    if data_dir:
        os.makedirs(data_dir, exist_ok=True)
        config_kwargs["save_dir"] = data_dir
        config_kwargs["app_data_dir"] = data_dir

    async with Agent(config_cls(**config_kwargs)) as agent:
        logging.info("Antigravity agent started in %s; dispatching goal prompt", workspace_dir)
        prompt = (
            f"Set up the workspace directory {workspace_dir} (your current working directory) "
            f"according to the following goal: {goal}"
        )
        response = await agent.chat(prompt)
        reply_text = await response.text()
        logging.info("Antigravity workspace setup completed with response:")
        print(reply_text)


def main(argv: list[str]) -> int:
    args = parse_args(argv)
    workspace_dir = os.path.abspath(args.workspace)

    if not args.goal:
        logging.info("No goal provided for Antigravity workspace bootstrap; skipping")
        return 0
    if not os.environ.get(API_KEY_ENV) and not os.environ.get(BASE_URL_ENV):
        logging.error(
            "neither %s nor %s is set; cannot run Antigravity workspace bootstrap",
            API_KEY_ENV,
            BASE_URL_ENV,
        )
        return 1
    if not os.path.isdir(workspace_dir):
        logging.error("Workspace directory %s does not exist", workspace_dir)
        return 1

    os.chdir(workspace_dir)
    logging.info("Starting Antigravity workspace bootstrap in %s with goal: %s", workspace_dir, args.goal)
    try:
        asyncio.run(run_bootstrap(args.goal, workspace_dir, args.data_dir))
    except Exception:
        logging.exception("Antigravity workspace setup failed")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
