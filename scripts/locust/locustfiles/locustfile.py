from locust import FastHttpUser, task, between
import uuid

class NaiveUploader(FastHttpUser):  
    wait_time = between(1, 3)
    @task(1)
    def upload_files(self):
      with open("1mb_file", "rb") as f:
          file_id = str(uuid.uuid4())
          self.client.post("/api/v1/binary", data=f.read(), headers={"X-Api-File-Name": file_id}, name="/api/v1/binary")
      