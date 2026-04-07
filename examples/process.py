import sys

def process():
    data = sys.stdin.read()
    if not data:
        print("No input data received.")
        return

    # Basic example processing: reverse text and count lines
    lines = data.strip().split('\n')
    processed_lines = [line[::-1] for line in lines]
    
    print(f"--- TEE Enclave Result ---")
    print(f"Processed {len(lines)} lines.")
    print("Reversed content:")
    for line in processed_lines:
        print(f"  {line}")

if __name__ == "__main__":
    process()
