import logging
import sys
from typing import Optional

from config.settings import settings


def setup_logger(name: Optional[str] = None) -> logging.Logger:
    """Set up logger with consistent formatting."""
    logger = logging.getLogger(name or __name__)
    
    if not logger.handlers:
        # Use stderr instead of stdout to avoid interfering with MCP protocol
        handler = logging.StreamHandler(sys.stderr)
        formatter = logging.Formatter(
            '%(asctime)s - %(name)s - %(levelname)s - %(message)s'
        )
        handler.setFormatter(formatter)
        logger.addHandler(handler)
        logger.setLevel(getattr(logging, settings.log_level.upper()))
        
        # Prevent propagation to root logger
        logger.propagate = False
    
    return logger