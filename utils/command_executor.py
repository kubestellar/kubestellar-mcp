import asyncio
import subprocess
from typing import Dict, List, Optional, Tuple

from utils.logger import setup_logger

logger = setup_logger(__name__)


class CommandExecutor:
    """Execute shell commands with proper error handling."""
    
    @staticmethod
    async def run_command(
        command: List[str],
        timeout: int = 300,
        cwd: Optional[str] = None,
        env: Optional[Dict[str, str]] = None,
        capture_output: bool = True
    ) -> Tuple[int, str, str]:
        """
        Run a command asynchronously.
        
        Returns:
            Tuple of (return_code, stdout, stderr)
        """
        try:
            logger.info(f"Executing command: {' '.join(command)}")
            
            process = await asyncio.create_subprocess_exec(
                *command,
                stdout=subprocess.PIPE if capture_output else None,
                stderr=subprocess.PIPE if capture_output else None,
                cwd=cwd,
                env=env
            )
            
            stdout, stderr = await asyncio.wait_for(
                process.communicate(),
                timeout=timeout
            )
            
            stdout_str = stdout.decode('utf-8') if stdout else ""
            stderr_str = stderr.decode('utf-8') if stderr else ""
            
            logger.debug(f"Command completed with return code: {process.returncode}")
            if stdout_str:
                logger.debug(f"STDOUT: {stdout_str}")
            if stderr_str and process.returncode != 0:
                logger.warning(f"STDERR: {stderr_str}")
            
            return process.returncode, stdout_str, stderr_str
            
        except asyncio.TimeoutError:
            logger.error(f"Command timed out after {timeout} seconds")
            return 1, "", f"Command timed out after {timeout} seconds"
        except Exception as e:
            logger.error(f"Command execution failed: {e}")
            return 1, "", str(e)
    
    @staticmethod
    def run_command_sync(
        command: List[str],
        timeout: int = 300,
        cwd: Optional[str] = None,
        env: Optional[Dict[str, str]] = None
    ) -> Tuple[int, str, str]:
        """Run a command synchronously."""
        try:
            logger.info(f"Executing command: {' '.join(command)}")
            
            result = subprocess.run(
                command,
                capture_output=True,
                text=True,
                timeout=timeout,
                cwd=cwd,
                env=env
            )
            
            logger.debug(f"Command completed with return code: {result.returncode}")
            return result.returncode, result.stdout, result.stderr
            
        except subprocess.TimeoutExpired:
            logger.error(f"Command timed out after {timeout} seconds")
            return 1, "", f"Command timed out after {timeout} seconds"
        except Exception as e:
            logger.error(f"Command execution failed: {e}")
            return 1, "", str(e)
