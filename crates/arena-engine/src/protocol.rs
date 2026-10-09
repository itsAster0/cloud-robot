use prost::Message;
use std::io::{self, Read, Write};
#[derive(Clone, PartialEq, Message)]
pub struct Envelope {
    #[prost(uint32, tag = "1")]
    pub version: u32,
    #[prost(string, tag = "2")]
    pub kind: String,
    #[prost(bytes = "vec", tag = "3")]
    pub payload: Vec<u8>,
}
pub const MAX_FRAME: usize = 16 * 1024 * 1024;
pub fn read(r: &mut impl Read) -> io::Result<Envelope> {
    let mut size = [0; 4];
    r.read_exact(&mut size)?;
    let n = u32::from_be_bytes(size) as usize;
    if n > MAX_FRAME {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "frame too large",
        ));
    }
    let mut bytes = vec![0; n];
    r.read_exact(&mut bytes)?;
    Envelope::decode(bytes.as_slice()).map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))
}
pub fn write(w: &mut impl Write, kind: &str, payload: &serde_json::Value) -> io::Result<()> {
    let envelope = Envelope {
        version: 4,
        kind: kind.into(),
        payload: serde_json::to_vec(payload)?,
    };
    let bytes = envelope.encode_to_vec();
    if bytes.len() > MAX_FRAME {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            "frame too large",
        ));
    }
    w.write_all(&(bytes.len() as u32).to_be_bytes())?;
    w.write_all(&bytes)
}
