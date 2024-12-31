import os
import requests
import logging
from urllib.parse import urlparse
import time

# Configure logging
logging.basicConfig(
    level=logging.DEBUG,
    format='%(asctime)s - %(levelname)s - %(message)s'
)
logger = logging.getLogger(__name__)

CHUNK_SIZE = 32 * 1024 * 1024  # 32MB chunks

def main():
    # Open and get file info
    try:
        file_path = "testfile"
        file_size = os.path.getsize(file_path)
        logger.debug(f"File size in bytes: {file_size}")
    except Exception as e:
        logger.fatal(f"Error accessing file: {e}")
        return

    # Create upload
    headers = {
        "Content-Type": "application/octet-stream",
        "Upload-Length": str(file_size),
        "Tus-Resumable": "1.0.0"
    }
    
    try:
        response = requests.post(
            "http://localhost:8080/api/v3/files",
            headers=headers
        )
        response.raise_for_status()
        
        location = response.headers.get("Location")
        file_id = location.split("/")[-1]
        logger.debug(f"Extracted file ID: {file_id}")
    except Exception as e:
        logger.fatal(f"Error creating upload: {e}")
        return

    while True:
        try:
            # Get current offset
            head_response = requests.head(
                f"http://localhost:8080/api/v3/files/{file_id}",
                headers={"Tus-Resumable": "1.0.0"}
            )
            head_response.raise_for_status()
            
            offset = int(head_response.headers.get("Upload-Offset", "0"))
            logger.debug(f"Current upload offset: {offset}")

            if offset >= file_size:
                logger.debug("File upload complete")
                break

            # Calculate chunk size for this iteration
            remaining_bytes = file_size - offset
            current_chunk_size = min(CHUNK_SIZE, remaining_bytes)

            # Open file and seek to offset
            with open(file_path, "rb") as f:
                f.seek(offset)
                chunk = f.read(current_chunk_size)

                logger.debug(f"Sending chunk: size={len(chunk)}, offset={offset}")

                # Upload chunk
                headers = {
                    "Content-Type": "application/offset+octet-stream",
                    "Tus-Resumable": "1.0.0",
                    "Upload-Offset": str(offset)
                }

                patch_response = requests.patch(
                    f"http://localhost:8080/api/v3/files/{file_id}",
                    headers=headers,
                    data=chunk
                )
                patch_response.raise_for_status()

                logger.debug(
                    f"Upload response: status={patch_response.status_code}, "
                    f"new_offset={patch_response.headers.get('Upload-Offset')}"
                )

        except Exception as e:
            logger.warning(f"Error during upload: {e}")
            time.sleep(1)  # Wait before retry
            continue

if __name__ == "__main__":
    main()
