with open("metrics.data") as file:
    lines = file.readlines()
    lines = [line.rstrip() for line in lines]
    print('|'.join(lines))
